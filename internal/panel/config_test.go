package panel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAppliesDefaultsAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yaml")
	data := []byte(`listen: "127.0.0.1:8091"
database:
  driver: "mysql"
security:
  backend_api_keys:
    - "backend-key"
session:
  ttl: "1h"
node_command:
  max_clock_skew: "1m"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GACCEL_PANEL_DATABASE_DSN", "user:pass@tcp(127.0.0.1:3306)/gaccel_panel")
	t.Setenv("GACCEL_PANEL_MASTER_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GACCEL_PANEL_SESSION_SECRET", "session-secret-session-secret")
	t.Setenv("GACCEL_PANEL_COMMAND_SECRET", "command-secret-command-secret")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Listen != "127.0.0.1:8091" {
		t.Fatalf("Listen = %q", cfg.Listen)
	}
	if cfg.Database.DSN == "" {
		t.Fatal("Database.DSN is empty")
	}
	if cfg.Session.CookieName != "gaccel_panel_session" {
		t.Fatalf("CookieName = %q", cfg.Session.CookieName)
	}
	if cfg.Deploy.DefaultNodeVersion != "latest" {
		t.Fatalf("DefaultNodeVersion = %q", cfg.Deploy.DefaultNodeVersion)
	}
}

func TestLoadConfigRejectsPlaceholders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yaml")
	data := []byte(`listen: "127.0.0.1:8091"
database:
  driver: "mysql"
  dsn: "user:pass@tcp(127.0.0.1:3306)/gaccel_panel"
security:
  master_key: "change-me-panel-master-key"
  backend_api_keys:
    - "backend-key"
session:
  secret: "session-secret-session-secret"
node_command:
  secret: "command-secret-command-secret"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig returned nil for placeholder master key")
	}
}
