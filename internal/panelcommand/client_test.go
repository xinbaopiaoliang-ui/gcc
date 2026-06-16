package panelcommand

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"gaccel-node/internal/config"
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

func nilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
