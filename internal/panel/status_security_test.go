package panel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSecurityOverviewJSONUsesEmptyArrays(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.MasterKey = "master-key"
	cfg.Security.BackendAPIKeys = []string{"backend-key"}
	cfg.Session.Secret = "session-secret"
	cfg.NodeCommand.Secret = "command-secret"
	cfg.CORS.AllowedOrigins = []string{"http://103.201.131.99:9788"}

	overview := BuildSecurityOverview(cfg, []PanelUser{{
		Role:   PanelUserRoleAdmin,
		Status: PanelUserStatusActive,
	}}, nil)

	data, err := json.Marshal(overview)
	if err != nil {
		t.Fatalf("marshal overview: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"warnings":[]`) {
		t.Fatalf("warnings must encode as []: %s", body)
	}
	if strings.Contains(body, `"cors_allowed_origins":null`) {
		t.Fatalf("cors origins must not encode as null: %s", body)
	}
}

func TestNodeSyncStatusJSONUsesEmptyRecommendations(t *testing.T) {
	now := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	reportedAt := now
	status := BuildNodeSyncStatus(Node{
		Status:               "online",
		LastReportAt:         &reportedAt,
		HMACSecretConfigured: true,
	}, nil, nil, now)

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal sync status: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"recommendations":[]`) {
		t.Fatalf("recommendations must encode as []: %s", body)
	}
}
