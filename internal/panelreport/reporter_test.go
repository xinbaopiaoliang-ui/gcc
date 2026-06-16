package panelreport

import (
	"testing"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
)

func TestBuildPayloadIncludesNodeAndVersion(t *testing.T) {
	cfg := config.Default()
	cfg.Node = config.NodeConfig{
		ID:     "node-hk-01",
		Region: "hk",
		Tags:   []string{"steam"},
		Labels: map[string]string{"line": "premium"},
	}
	cfg.Server.Listen = ":5555"
	cfg.Server.ALPN = "gaccel/1"

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	payload := BuildPayload(cfg, metrics.Snapshot{ActiveQUICConnections: 3}, "0.3.2-test", now)

	if payload.Version != "0.3.2-test" {
		t.Fatalf("Version = %q, want 0.3.2-test", payload.Version)
	}
	if payload.Node.ID != "node-hk-01" {
		t.Fatalf("Node.ID = %q, want node-hk-01", payload.Node.ID)
	}
	if payload.Server.Listen != ":5555" {
		t.Fatalf("Server.Listen = %q, want :5555", payload.Server.Listen)
	}
	if payload.Metrics.ActiveQUICConnections != 3 {
		t.Fatalf("ActiveQUICConnections = %d, want 3", payload.Metrics.ActiveQUICConnections)
	}
	if !payload.Timestamp.Equal(now) {
		t.Fatalf("Timestamp = %s, want %s", payload.Timestamp, now)
	}
}
