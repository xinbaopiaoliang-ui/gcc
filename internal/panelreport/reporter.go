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
)

type Reporter struct {
	cfg       *config.Manager
	logger    *slog.Logger
	collector *metrics.Collector
	version   string
	client    *http.Client
}

type Payload struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Node      config.NodeConfig `json:"node"`
	Server    ServerInfo        `json:"server"`
	Metrics   metrics.Snapshot  `json:"metrics"`
}

type ServerInfo struct {
	Listen string `json:"listen"`
	ALPN   string `json:"alpn"`
}

func New(cfg *config.Manager, logger *slog.Logger, collector *metrics.Collector, version string) *Reporter {
	return &Reporter{
		cfg:       cfg,
		logger:    logger.With("component", "panel-report"),
		collector: collector,
		version:   version,
		client:    &http.Client{},
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

	payload := BuildPayload(cfg, r.collector.Snapshot(), r.version, time.Now())
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
	r.logger.Debug("panel report sent", "url", cfg.Panel.ReportURL, "status", resp.Status)
	return nil
}

func BuildPayload(cfg *config.Config, snapshot metrics.Snapshot, version string, now time.Time) Payload {
	return Payload{
		Status:    "ok",
		Version:   version,
		Timestamp: now.UTC(),
		Node:      cfg.Node,
		Server: ServerInfo{
			Listen: cfg.Server.Listen,
			ALPN:   cfg.Server.ALPN,
		},
		Metrics: snapshot,
	}
}
