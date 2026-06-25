package panelreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/panelcommand"
	"gaccel-node/internal/sessions"
)

type Reporter struct {
	cfg            *config.Manager
	logger         *slog.Logger
	collector      *metrics.Collector
	sessions       *sessions.Registry
	version        string
	client         *http.Client
	commandResults CommandResultSnapshotter
}

type CommandResultSnapshotter interface {
	Snapshot() []panelcommand.CommandResult
}

type Payload struct {
	Status        string                       `json:"status"`
	Version       string                       `json:"version"`
	Timestamp     time.Time                    `json:"timestamp"`
	Node          config.NodeConfig            `json:"node"`
	Server        ServerInfo                   `json:"server"`
	RoutePolicies RoutePoliciesInfo            `json:"route_policies"`
	Metrics       metrics.Snapshot             `json:"metrics"`
	Sessions      []sessions.Snapshot          `json:"sessions,omitempty"`
	SessionEvents []sessions.Event             `json:"session_events,omitempty"`
	PanelCommands []panelcommand.CommandResult `json:"panel_commands,omitempty"`
}

type ServerInfo struct {
	Listen string `json:"listen"`
	ALPN   string `json:"alpn"`
}

type RoutePoliciesInfo struct {
	Revision    string `json:"revision"`
	PolicyCount int    `json:"policy_count"`
}

func New(cfg *config.Manager, logger *slog.Logger, collector *metrics.Collector, sessionRegistry *sessions.Registry, version string, commandResults ...CommandResultSnapshotter) *Reporter {
	var resultSnapshotter CommandResultSnapshotter
	if len(commandResults) > 0 {
		resultSnapshotter = commandResults[0]
	}
	return &Reporter{
		cfg:            cfg,
		logger:         logger.With("component", "panel-report"),
		collector:      collector,
		sessions:       sessionRegistry,
		version:        version,
		client:         &http.Client{},
		commandResults: resultSnapshotter,
	}
}

func (r *Reporter) Run(ctx context.Context) {
	for {
		cfg := r.cfg.Current()
		if cfg.Panel.ReportURL != "" {
			if err := r.report(ctx, cfg); err != nil {
				r.logger.Warn("panel report failed", "error", err)
			}
		}
		interval := cfg.Panel.Interval
		if interval <= 0 {
			interval = 30 * time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *Reporter) report(parent context.Context, cfg *config.Config) error {
	timeout := cfg.Panel.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	var commandResults []panelcommand.CommandResult
	if r.commandResults != nil {
		commandResults = r.commandResults.Snapshot()
	}
	activeSessions := []sessions.Snapshot{}
	sessionEvents := []sessions.Event{}
	if r.sessions != nil {
		activeSessions = r.sessions.Snapshot()
		sessionEvents = r.sessions.PendingEvents(2000)
	}
	payload := BuildPayloadWithSessions(cfg, r.collector.Snapshot(), activeSessions, sessionEvents, r.version, time.Now(), commandResults)
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Panel.ReportURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Panel.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gaccel-node/"+r.version)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("panel returned %s", resp.Status)
	}
	if r.sessions != nil && len(sessionEvents) > 0 {
		r.sessions.AckEvents(sessionEvents[len(sessionEvents)-1].Sequence)
	}
	r.logger.Debug("panel report sent", "url", cfg.Panel.ReportURL, "status", resp.Status)
	return nil
}

func BuildPayload(cfg *config.Config, snapshot metrics.Snapshot, version string, now time.Time, commandResults ...[]panelcommand.CommandResult) Payload {
	return BuildPayloadWithSessions(cfg, snapshot, nil, nil, version, now, commandResults...)
}

func BuildPayloadWithSessions(cfg *config.Config, snapshot metrics.Snapshot, activeSessions []sessions.Snapshot, sessionEvents []sessions.Event, version string, now time.Time, commandResults ...[]panelcommand.CommandResult) Payload {
	payload := Payload{
		Status:    "ok",
		Version:   version,
		Timestamp: now.UTC(),
		Node:      cfg.Node,
		Server: ServerInfo{
			Listen: cfg.Server.Listen,
			ALPN:   cfg.Server.ALPN,
		},
		RoutePolicies: RoutePoliciesInfo{
			Revision:    cfg.RoutePolicies.Revision,
			PolicyCount: len(cfg.RoutePolicies.Policies),
		},
		Metrics: snapshot,
	}
	if activeSessions != nil {
		payload.Sessions = activeSessions
	}
	if sessionEvents != nil {
		payload.SessionEvents = sessionEvents
	}
	if len(commandResults) > 0 {
		payload.PanelCommands = commandResults[0]
	}
	return payload
}
