package config

import "testing"

func TestNormalizeNodeMetadata(t *testing.T) {
	node := NodeConfig{
		ID:     " node-hk-01 ",
		Region: " hk ",
		Tags:   []string{" steam ", "", "quic", "steam"},
		Labels: map[string]string{
			" provider ": " example ",
			"empty":      "",
			"":           "ignored",
		},
	}

	normalizeNode(&node)

	if node.ID != "node-hk-01" {
		t.Fatalf("ID = %q, want node-hk-01", node.ID)
	}
	if node.Region != "hk" {
		t.Fatalf("Region = %q, want hk", node.Region)
	}
	if len(node.Tags) != 2 || node.Tags[0] != "steam" || node.Tags[1] != "quic" {
		t.Fatalf("Tags = %#v, want [steam quic]", node.Tags)
	}
	if got := node.Labels["provider"]; got != "example" {
		t.Fatalf("Labels[provider] = %q, want example", got)
	}
	if _, ok := node.Labels["empty"]; ok {
		t.Fatal("empty label value was not removed")
	}
}

func TestLoadDataAcceptsRoutePolicies(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

route_policies:
  revision: "r1"
  policies:
    - policy_id: "steam-web-v1"
      game_id: "steam"
      allow_tcp: true
      allow_udp: false
      rules:
        - rule_id: "steam-store-tcp-443"
          network: "tcp"
          target_type: "domain"
          target_value: "store.steampowered.com"
          port_start: 443
          port_end: 443
          action: "quic_relay"
`)

	cfg, err := LoadData(data)
	if err != nil {
		t.Fatalf("LoadData returned error: %v", err)
	}
	if cfg.RoutePolicies.Revision != "r1" {
		t.Fatalf("RoutePolicies.Revision = %q, want r1", cfg.RoutePolicies.Revision)
	}
	if len(cfg.RoutePolicies.Policies) != 1 {
		t.Fatalf("policies = %d, want 1", len(cfg.RoutePolicies.Policies))
	}
}

func TestLoadDataRejectsInvalidRoutePolicyMode(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

route_policies:
  mode: "unknown"
`)

	if _, err := LoadData(data); err == nil {
		t.Fatal("LoadData returned nil for invalid route_policies.mode")
	}
}

func TestEffectiveRoutePoliciesMode(t *testing.T) {
	if got := EffectiveRoutePoliciesMode(RoutePoliciesConfig{}); got != RoutePoliciesModeStrict {
		t.Fatalf("empty mode = %q, want %q", got, RoutePoliciesModeStrict)
	}
	if got := EffectiveRoutePoliciesMode(RoutePoliciesConfig{Mode: RoutePoliciesModeClientDecision}); got != RoutePoliciesModeClientDecision {
		t.Fatalf("explicit mode = %q, want %q", got, RoutePoliciesModeClientDecision)
	}
	if got := EffectiveRoutePoliciesMode(RoutePoliciesConfig{
		Policies: []RoutePolicyConfig{{PolicyID: "policy-1", GameID: "game-1"}},
	}); got != RoutePoliciesModeClientDecision {
		t.Fatalf("legacy policy bundle mode = %q, want %q", got, RoutePoliciesModeClientDecision)
	}
}

func TestLoadDataRejectsInvalidRoutePolicyRule(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

route_policies:
  policies:
    - policy_id: "bad-policy"
      game_id: "bad"
      rules:
        - rule_id: "bad-rule"
          network: "udp"
          target_type: "cidr"
          target_value: "not-cidr"
          port_start: 1
          port_end: 1
          action: "quic_relay"
`)

	if _, err := LoadData(data); err == nil {
		t.Fatal("LoadData returned nil for invalid route policy")
	}
}

func TestLoadDataMigratesLegacyDefaultAllowedTCPPorts(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

security:
  allowed_tcp_ports:
    - "80"
    - "443"
    - "1935"
    - "5222"
    - "27000-65535"
`)

	cfg, err := LoadData(data)
	if err != nil {
		t.Fatalf("LoadData returned error: %v", err)
	}
	if len(cfg.Security.AllowedTCPPorts) != 1 || cfg.Security.AllowedTCPPorts[0] != "1-65535" {
		t.Fatalf("AllowedTCPPorts = %#v, want [1-65535]", cfg.Security.AllowedTCPPorts)
	}
}

func TestLoadDataPreservesCustomAllowedTCPPorts(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

security:
  allowed_tcp_ports:
    - "80"
    - "443"
`)

	cfg, err := LoadData(data)
	if err != nil {
		t.Fatalf("LoadData returned error: %v", err)
	}
	if len(cfg.Security.AllowedTCPPorts) != 2 || cfg.Security.AllowedTCPPorts[0] != "80" || cfg.Security.AllowedTCPPorts[1] != "443" {
		t.Fatalf("AllowedTCPPorts = %#v, want [80 443]", cfg.Security.AllowedTCPPorts)
	}
}

func TestLoadDataAppliesUDPSendQueueSizeDefault(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"
`)

	cfg, err := LoadData(data)
	if err != nil {
		t.Fatalf("LoadData returned error: %v", err)
	}
	if cfg.Limits.UDPSendQueueSize != 1024 {
		t.Fatalf("UDPSendQueueSize = %d, want 1024", cfg.Limits.UDPSendQueueSize)
	}
}

func TestLoadDataRejectsInvalidUDPSendQueueSize(t *testing.T) {
	data := []byte(`server:
  listen: ":5555"
  alpn: "gaccel/1"
  cert_file: "/tmp/cert.pem"
  key_file: "/tmp/key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

limits:
  udp_send_queue_size: 8
`)

	if _, err := LoadData(data); err == nil {
		t.Fatal("LoadData returned nil for invalid udp_send_queue_size")
	}
}
