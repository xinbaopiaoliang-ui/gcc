package panelcommand

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/upgrade"
)

const (
	HeaderTimestamp = "X-Gaccel-Timestamp"
	HeaderNonce     = "X-Gaccel-Nonce"
	HeaderSignature = "X-Gaccel-Signature"

	CommandNoop         = "noop"
	CommandConfigReload = "config_reload"
	CommandApplyConfig  = "apply_config"
	CommandStageUpgrade = "stage_upgrade"
)

type Client struct {
	cfg       *config.Manager
	logger    *slog.Logger
	version   string
	client    *http.Client
	nonces    *nonceCache
	now       func() time.Time
	collector ResultCollector
	upgrader  UpgradeStager
}

type ResultCollector interface {
	Record(CommandResult)
}

type UpgradeStager interface {
	Stage(context.Context, config.UpgradeConfig, upgrade.Request) (*upgrade.Result, error)
}

type Envelope struct {
	Commands []Command `json:"commands"`
}

type Command struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	IssuedAt  time.Time       `json:"issued_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type CommandResult struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
	Details    any       `json:"details,omitempty"`
	ExecutedAt time.Time `json:"executed_at"`
}

type ApplyConfigPayload struct {
	SHA256     string `json:"sha256"`
	ConfigYAML string `json:"config_yaml"`
}

func New(cfg *config.Manager, logger *slog.Logger, version string) *Client {
	return &Client{
		cfg:       cfg,
		logger:    logger.With("component", "panel-command"),
		version:   version,
		client:    &http.Client{},
		nonces:    newNonceCache(),
		now:       time.Now,
		collector: noopCollector{},
		upgrader:  upgrade.NewStager(),
	}
}

func (c *Client) Run(ctx context.Context) {
	for {
		cfg := c.cfg.Current()
		if cfg.Panel.CommandURL != "" {
			if err := c.poll(ctx, cfg); err != nil {
				c.logger.Warn("panel command poll failed", "error", err)
			}
		}
		interval := cfg.Panel.CommandInterval
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

func (c *Client) poll(parent context.Context, cfg *config.Config) error {
	timeout := cfg.Panel.CommandTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, commandURL(cfg), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Panel.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gaccel-node/"+c.version)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("panel returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := c.verifyResponse(cfg, resp.Header, body); err != nil {
		return err
	}

	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	for _, command := range envelope.Commands {
		result := c.execute(ctx, cfg, command)
		c.collector.Record(result)
		if result.OK {
			c.logger.Info("panel command executed", "id", result.ID, "type", result.Type)
		} else {
			c.logger.Warn("panel command failed", "id", result.ID, "type", result.Type, "error", result.Error)
		}
	}
	return nil
}

func commandURL(cfg *config.Config) string {
	parsed, err := url.Parse(cfg.Panel.CommandURL)
	if err != nil {
		return cfg.Panel.CommandURL
	}
	query := parsed.Query()
	if cfg.Node.ID != "" && query.Get("node_id") == "" {
		query.Set("node_id", cfg.Node.ID)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (c *Client) verifyResponse(cfg *config.Config, header http.Header, body []byte) error {
	timestampValue := strings.TrimSpace(header.Get(HeaderTimestamp))
	nonce := strings.TrimSpace(header.Get(HeaderNonce))
	signature := strings.TrimSpace(header.Get(HeaderSignature))
	if timestampValue == "" || nonce == "" || signature == "" {
		return errors.New("panel command response is missing signature headers")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, timestampValue)
	if err != nil {
		return fmt.Errorf("panel command timestamp is invalid: %w", err)
	}
	skew := cfg.Panel.CommandMaxClockSkew
	if skew <= 0 {
		skew = 2 * time.Minute
	}
	now := c.now()
	if timestamp.Before(now.Add(-skew)) || timestamp.After(now.Add(skew)) {
		return errors.New("panel command timestamp is outside the allowed clock skew")
	}
	if !c.nonces.add(nonce, now.Add(skew)) {
		return errors.New("panel command nonce was already used")
	}
	expected := SignBody(cfg.Panel.CommandSecret, timestampValue, nonce, body)
	if !signatureEqual(signature, expected) {
		return errors.New("panel command signature mismatch")
	}
	return nil
}

func (c *Client) execute(ctx context.Context, cfg *config.Config, command Command) CommandResult {
	now := c.now().UTC()
	result := CommandResult{
		ID:         strings.TrimSpace(command.ID),
		Type:       strings.TrimSpace(command.Type),
		ExecutedAt: now,
	}
	if result.ID == "" {
		result.Error = "command id is required"
		return result
	}
	if result.Type == "" {
		result.Error = "command type is required"
		return result
	}
	if !command.ExpiresAt.IsZero() && now.After(command.ExpiresAt) {
		result.Error = "command expired"
		return result
	}
	if !command.IssuedAt.IsZero() {
		skew := cfg.Panel.CommandMaxClockSkew
		if skew <= 0 {
			skew = 2 * time.Minute
		}
		if command.IssuedAt.After(now.Add(skew)) {
			result.Error = "command issued_at is in the future"
			return result
		}
	}

	switch result.Type {
	case CommandNoop:
		result.OK = true
	case CommandConfigReload:
		if _, err := c.cfg.Reload(); err != nil {
			result.Error = err.Error()
			return result
		}
		result.OK = true
	case CommandApplyConfig:
		var payload ApplyConfigPayload
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			result.Error = "invalid apply_config payload: " + err.Error()
			return result
		}
		applied, err := c.cfg.ApplyPackage([]byte(payload.ConfigYAML), payload.SHA256)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.OK = true
		result.Details = map[string]string{
			"sha256":      applied.SHA256,
			"backup_path": applied.BackupPath,
		}
	case CommandStageUpgrade:
		var payload upgrade.Request
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			result.Error = "invalid stage_upgrade payload: " + err.Error()
			return result
		}
		staged, err := c.upgrader.Stage(ctx, cfg.Upgrade, payload)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.OK = true
		result.Details = map[string]any{
			"version":       staged.Version,
			"sha256":        staged.SHA256,
			"size_bytes":    staged.SizeBytes,
			"file_path":     staged.FilePath,
			"manifest_path": staged.ManifestPath,
			"staged_at":     staged.StagedAt,
		}
	default:
		result.Error = "unsupported command type"
	}
	return result
}

func SignBody(secret, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func signatureEqual(got, expected string) bool {
	got = strings.TrimSpace(strings.TrimPrefix(got, "v1="))
	expected = strings.TrimSpace(strings.TrimPrefix(expected, "v1="))
	gotBytes, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(gotBytes, expectedBytes) == 1
}

type nonceCache struct {
	mu     sync.Mutex
	nonces map[string]time.Time
}

func newNonceCache() *nonceCache {
	return &nonceCache{nonces: map[string]time.Time{}}
}

func (c *nonceCache) add(nonce string, expiresAt time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, expires := range c.nonces {
		if !expires.After(now) {
			delete(c.nonces, key)
		}
	}
	if _, ok := c.nonces[nonce]; ok {
		return false
	}
	c.nonces[nonce] = expiresAt
	return true
}

type noopCollector struct{}

func (noopCollector) Record(CommandResult) {}
