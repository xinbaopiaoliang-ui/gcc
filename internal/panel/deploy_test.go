package panel

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewDeployNodeTaskStoresEncryptedHMACSecret(t *testing.T) {
	box, err := NewSecretBox("test-master-key-with-enough-entropy")
	if err != nil {
		t.Fatalf("NewSecretBox: %v", err)
	}
	task, err := NewDeployNodeTask("node-hk-01", DeployNodeTaskRequest{
		Version:      "latest",
		HMACSecret:   "secret-hmac-value-123456",
		PanelBaseURL: "http://panel.example",
	}, "latest", time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC), box)
	if err != nil {
		t.Fatalf("NewDeployNodeTask: %v", err)
	}
	raw := string(mustJSONRaw(t, task.RequestJSON))
	if strings.Contains(raw, "secret-hmac-value-123456") {
		t.Fatalf("request_json leaked plaintext hmac secret: %s", raw)
	}
	cfg := DefaultConfig()
	cfg.Security.MasterKey = "test-master-key-with-enough-entropy"
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	server := NewServer(cfg, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), "test")
	decoded, err := server.deployRequestFromTask(NodeTask{RequestJSON: mustJSONRaw(t, task.RequestJSON)})
	if err != nil {
		t.Fatalf("deployRequestFromTask: %v", err)
	}
	if decoded.HMACSecret != "secret-hmac-value-123456" {
		t.Fatalf("decoded hmac secret = %q", decoded.HMACSecret)
	}
}

func TestDeployNodeTaskCanUseNodeStoredHMACSecret(t *testing.T) {
	box, err := NewSecretBox("test-master-key-with-enough-entropy")
	if err != nil {
		t.Fatalf("NewSecretBox: %v", err)
	}
	task, err := NewDeployNodeTask("node-hk-01", DeployNodeTaskRequest{
		Version:      "v0.6.4",
		PanelBaseURL: "http://panel.example",
	}, "latest", time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC), box)
	if err != nil {
		t.Fatalf("NewDeployNodeTask: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Security.MasterKey = "test-master-key-with-enough-entropy"
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	server := NewServer(cfg, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), "test")
	decoded, err := server.deployRequestFromTask(NodeTask{RequestJSON: mustJSONRaw(t, task.RequestJSON)})
	if err != nil {
		t.Fatalf("deployRequestFromTask: %v", err)
	}
	if decoded.HMACSecret != "" {
		t.Fatalf("deploy request should not carry hmac secret, got %q", decoded.HMACSecret)
	}
	encrypted, err := box.Encrypt("backend-issued-node-secret-123456")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	resolved, err := server.resolveDeployHMACSecret(Node{
		NodeID:               "node-hk-01",
		HMACSecretConfigured: true,
		HMACSecretEncrypted:  encrypted,
		HMACSecretSource:     "backend",
		HMACSecretUpdatedAt:  ptrTime(time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)),
	}, decoded)
	if err != nil {
		t.Fatalf("resolveDeployHMACSecret: %v", err)
	}
	if resolved != "backend-issued-node-secret-123456" {
		t.Fatalf("resolved hmac secret = %q", resolved)
	}
}

func TestNewUpdateNodeTaskDefaultsVersion(t *testing.T) {
	task, err := NewUpdateNodeTask("node-hk-01", UpdateNodeTaskRequest{}, "v0.5.4", time.Date(2026, 6, 17, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewUpdateNodeTask: %v", err)
	}
	if task.Type != TaskTypeUpdateNode {
		t.Fatalf("task type = %q, want %q", task.Type, TaskTypeUpdateNode)
	}
	decoded, err := updateRequestFromTask(NodeTask{RequestJSON: mustJSONRaw(t, task.RequestJSON)})
	if err != nil {
		t.Fatalf("updateRequestFromTask: %v", err)
	}
	if decoded.Version != "v0.5.4" {
		t.Fatalf("version = %q, want v0.5.4", decoded.Version)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func mustJSONRaw(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := jsonRaw(value)
	if err != nil {
		t.Fatalf("jsonRaw: %v", err)
	}
	return raw
}
