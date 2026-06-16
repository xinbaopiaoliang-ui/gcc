package panelcommand

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/upgrade"
)

func TestSignBodyAndSignatureEqual(t *testing.T) {
	body := []byte(`{"commands":[]}`)
	signature := SignBody("secret", "2026-06-16T12:00:00Z", "nonce-1", body)
	if !signatureEqual(signature, signature) {
		t.Fatal("signatureEqual returned false for the same signature")
	}
	if signatureEqual("v1=deadbeef", signature) {
		t.Fatal("signatureEqual returned true for a mismatched signature")
	}
}

func TestVerifyResponseRejectsReplay(t *testing.T) {
	cfg := config.Default()
	cfg.Panel.CommandSecret = "secret"
	cfg.Panel.CommandMaxClockSkew = time.Minute
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	client := New(nil, nilLogger(), "test")
	client.now = func() time.Time { return now }

	body := []byte(`{"commands":[]}`)
	timestamp := now.Format(time.RFC3339Nano)
	header := http.Header{}
	header.Set(HeaderTimestamp, timestamp)
	header.Set(HeaderNonce, "nonce-1")
	header.Set(HeaderSignature, SignBody(cfg.Panel.CommandSecret, timestamp, "nonce-1", body))

	if err := client.verifyResponse(cfg, header, body); err != nil {
		t.Fatalf("verifyResponse returned error: %v", err)
	}
	if err := client.verifyResponse(cfg, header, body); err == nil {
		t.Fatal("verifyResponse returned nil for replayed nonce")
	}
}

func TestCommandURLAddsNodeID(t *testing.T) {
	cfg := config.Default()
	cfg.Panel.CommandURL = "https://panel.example.com/api/commands?limit=10"
	cfg.Node.ID = "node-hk-01"

	got := commandURL(cfg)
	want := "https://panel.example.com/api/commands?limit=10&node_id=node-hk-01"
	if got != want {
		t.Fatalf("commandURL = %q, want %q", got, want)
	}
}

func TestExecuteApplyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldData := panelCommandTestConfig("node-old")
	newData := panelCommandTestConfig("node-new")
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	client := New(manager, nilLogger(), "test")
	client.now = func() time.Time { return time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC) }

	payload, err := json.Marshal(ApplyConfigPayload{
		SHA256:     panelCommandSHA256(newData),
		ConfigYAML: string(newData),
	})
	if err != nil {
		t.Fatal(err)
	}
	result := client.execute(context.Background(), manager.Current(), Command{
		ID:        "cmd-apply-config-1",
		Type:      CommandApplyConfig,
		IssuedAt:  client.now(),
		ExpiresAt: client.now().Add(time.Minute),
		Payload:   payload,
	})
	if !result.OK {
		t.Fatalf("apply_config failed: %s", result.Error)
	}
	if manager.Current().Node.ID != "node-new" {
		t.Fatalf("node id = %q, want node-new", manager.Current().Node.ID)
	}
}

func TestExecuteStageUpgrade(t *testing.T) {
	cfg := config.Default()
	cfg.Upgrade.StageDir = t.TempDir()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	client := New(nil, nilLogger(), "test")
	client.now = func() time.Time { return now }
	client.upgrader = fakeUpgradeStager{
		result: &upgrade.Result{
			Version:      "0.3.3",
			URL:          "https://example.com/pkg.tar.gz",
			FilePath:     "/stage/pkg.tar.gz",
			ManifestPath: "/stage/manifest.json",
			SHA256:       "abc",
			SizeBytes:    123,
			StagedAt:     now,
		},
	}

	payload, err := json.Marshal(upgrade.Request{
		Version: "0.3.3",
		URL:     "https://example.com/pkg.tar.gz",
		SHA256:  "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := client.execute(context.Background(), cfg, Command{
		ID:        "cmd-stage-upgrade-1",
		Type:      CommandStageUpgrade,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Minute),
		Payload:   payload,
	})
	if !result.OK {
		t.Fatalf("stage_upgrade failed: %s", result.Error)
	}
}

func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeUpgradeStager struct {
	result *upgrade.Result
	err    error
}

func (f fakeUpgradeStager) Stage(context.Context, config.UpgradeConfig, upgrade.Request) (*upgrade.Result, error) {
	return f.result, f.err
}

func panelCommandTestConfig(nodeID string) []byte {
	return []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

node:
  id: "` + nodeID + `"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"
`)
}

func panelCommandSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
