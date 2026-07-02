package panelreport

import (
	"testing"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/panelcommand"
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
	cfg.RoutePolicies = config.RoutePoliciesConfig{
		Revision: "20260616.1",
		Mode:     config.RoutePoliciesModeClientDecision,
		Policies: []config.RoutePolicyConfig{
			{PolicyID: "steam-web-v1", GameID: "steam"},
		},
	}

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
	if payload.RoutePolicies.Revision != "20260616.1" {
		t.Fatalf("RoutePolicies.Revision = %q, want 20260616.1", payload.RoutePolicies.Revision)
	}
	if payload.RoutePolicies.Mode != config.RoutePoliciesModeClientDecision {
		t.Fatalf("RoutePolicies.Mode = %q, want client_decision", payload.RoutePolicies.Mode)
	}
	if payload.RoutePolicies.PolicyCount != 1 {
		t.Fatalf("RoutePolicies.PolicyCount = %d, want 1", payload.RoutePolicies.PolicyCount)
	}
	if !payload.Timestamp.Equal(now) {
		t.Fatalf("Timestamp = %s, want %s", payload.Timestamp, now)
	}
}

func TestBuildPayloadIncludesCommandResults(t *testing.T) {
	cfg := config.Default()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	results := []panelcommand.CommandResult{
		{
			ID:         "cmd-stage-upgrade-1",
			Type:       panelcommand.CommandStageUpgrade,
			OK:         true,
			ExecutedAt: now,
			Details: map[string]any{
				"version": "0.3.3",
			},
		},
	}

	payload := BuildPayload(cfg, metrics.Snapshot{}, "0.3.3-test", now, results)
	if len(payload.PanelCommands) != 1 {
		t.Fatalf("len(PanelCommands) = %d, want 1", len(payload.PanelCommands))
	}
	if payload.PanelCommands[0].ID != "cmd-stage-upgrade-1" {
		t.Fatalf("PanelCommands[0].ID = %q", payload.PanelCommands[0].ID)
	}
}
