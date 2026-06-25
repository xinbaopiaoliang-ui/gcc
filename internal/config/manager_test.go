package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPackageWritesConfigAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldData := testConfigData("node-old")
	newData := testConfigData("node-new")
	if err := os.WriteFile(path, oldData, 0o640); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.ApplyPackage(newData, testSHA256(newData))
	if err != nil {
		t.Fatalf("ApplyPackage returned error: %v", err)
	}
	if result.SHA256 != testSHA256(newData) {
		t.Fatalf("SHA256 = %q, want %q", result.SHA256, testSHA256(newData))
	}
	if manager.Current().Node.ID != "node-new" {
		t.Fatalf("current node id = %q, want node-new", manager.Current().Node.ID)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `id: "node-new"`) {
		t.Fatalf("new config was not written: %s", written)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(backup), `id: "node-old"`) {
		t.Fatalf("backup does not contain old config: %s", backup)
	}
}

func TestApplyPackageRejectsHashMismatchAndKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldData := testConfigData("node-old")
	newData := testConfigData("node-new")
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ApplyPackage(newData, strings.Repeat("0", 64)); err == nil {
		t.Fatal("ApplyPackage returned nil for hash mismatch")
	}
	if manager.Current().Node.ID != "node-old" {
		t.Fatalf("current node id = %q, want node-old", manager.Current().Node.ID)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(oldData) {
		t.Fatal("config file changed after rejected package")
	}
}

func TestApplyPackageRejectsInvalidConfigAndKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldData := testConfigData("node-old")
	badData := []byte("server:\n  listen: ':5555'\n")
	if err := os.WriteFile(path, oldData, 0o600); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ApplyPackage(badData, testSHA256(badData)); err == nil {
		t.Fatal("ApplyPackage returned nil for invalid config")
	}
	if manager.Current().Node.ID != "node-old" {
		t.Fatalf("current node id = %q, want node-old", manager.Current().Node.ID)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(oldData) {
		t.Fatal("config file changed after invalid package")
	}
}

func TestApplyRoutePoliciesWritesOnlyPolicyBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldData := testConfigData("node-old")
	if err := os.WriteFile(path, oldData, 0o640); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	policyData := []byte(`route_policies:
  revision: "r2"
  policies:
    - policy_id: "steam-web-v1"
      game_id: "steam"
      rules:
        - rule_id: "steam-store-tcp-443"
          network: "tcp"
          target_type: "domain"
          target_value: "store.steampowered.com"
          port_start: 443
          port_end: 443
          action: "quic_relay"
`)
	result, err := manager.ApplyRoutePolicies(policyData, testSHA256(policyData))
	if err != nil {
		t.Fatalf("ApplyRoutePolicies returned error: %v", err)
	}
	if result.Config.Node.ID != "node-old" {
		t.Fatalf("node id = %q, want node-old", result.Config.Node.ID)
	}
	if result.Config.RoutePolicies.Revision != "r2" {
		t.Fatalf("route policy revision = %q, want r2", result.Config.RoutePolicies.Revision)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `policy_id: "steam-web-v1"`) {
		t.Fatalf("written config missing policy: %s", written)
	}
	if !strings.Contains(string(written), `id: "node-old"`) {
		t.Fatalf("written config lost node data: %s", written)
	}
}

func TestApplyRoutePoliciesRejectsHashMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	oldData := testConfigData("node-old")
	if err := os.WriteFile(path, oldData, 0o640); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	policyData := []byte("revision: r2\npolicies: []\n")
	if _, err := manager.ApplyRoutePolicies(policyData, strings.Repeat("0", 64)); err == nil {
		t.Fatal("ApplyRoutePolicies returned nil for hash mismatch")
	}
	if manager.Current().RoutePolicies.Revision != "" {
		t.Fatalf("route policy revision changed to %q", manager.Current().RoutePolicies.Revision)
	}
}

func testConfigData(nodeID string) []byte {
	return []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

node:
  id: "` + nodeID + `"
  region: "test"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"
`)
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
