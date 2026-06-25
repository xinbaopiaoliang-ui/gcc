package routepolicy

import (
	"errors"
	"testing"

	"gaccel-node/internal/auth"
	"gaccel-node/internal/config"
	"gaccel-node/internal/protocol"
)

func TestEvaluateAllowsConfiguredTCPRule(t *testing.T) {
	cfg := testPolicies()
	principal := &auth.Principal{
		UserID:         "user-1",
		GameIDs:        []string{"steam"},
		PolicyIDs:      []string{"steam-web-v1"},
		ConfigRevision: "20260616.1",
	}
	metadata := protocol.FlowMetadata{
		GameID:               "steam",
		PolicyID:             "steam-web-v1",
		RuleID:               "steam-store-tcp-443",
		Network:              "tcp",
		ClientConfigRevision: "20260616.1",
	}

	match, err := Evaluate(cfg, principal, metadata, "tcp", "store.steampowered.com", 443)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if match.PolicyID != "steam-web-v1" || match.RuleID != "steam-store-tcp-443" || match.GameID != "steam" {
		t.Fatalf("match = %#v", match)
	}
}

func TestEvaluateDeniesUnauthorizedPolicy(t *testing.T) {
	cfg := testPolicies()
	principal := &auth.Principal{
		UserID:    "user-1",
		GameIDs:   []string{"steam"},
		PolicyIDs: []string{"other-policy"},
	}
	metadata := protocol.FlowMetadata{
		GameID:               "steam",
		PolicyID:             "steam-web-v1",
		RuleID:               "steam-store-tcp-443",
		Network:              "tcp",
		ClientConfigRevision: "20260616.1",
	}

	if _, err := Evaluate(cfg, principal, metadata, "tcp", "store.steampowered.com", 443); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Evaluate error = %v, want ErrPolicyDenied", err)
	}
}

func TestEvaluateDeniesTargetMismatch(t *testing.T) {
	cfg := testPolicies()
	principal := &auth.Principal{UserID: "user-1"}
	metadata := protocol.FlowMetadata{
		GameID:               "steam",
		PolicyID:             "steam-web-v1",
		RuleID:               "steam-store-tcp-443",
		Network:              "tcp",
		ClientConfigRevision: "20260616.1",
	}

	if _, err := Evaluate(cfg, principal, metadata, "tcp", "example.com", 443); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Evaluate error = %v, want ErrPolicyDenied", err)
	}
}

func TestEvaluateAllowsCIDRUDPRule(t *testing.T) {
	cfg := config.RoutePoliciesConfig{
		Revision: "r1",
		Policies: []config.RoutePolicyConfig{
			{
				PolicyID: "game-udp-v1",
				GameID:   "game",
				Rules: []config.RoutePolicyRuleConfig{
					{
						RuleID:      "game-udp-cidr",
						Network:     "udp",
						TargetType:  "cidr",
						TargetValue: "103.201.131.0/24",
						PortStart:   15555,
						PortEnd:     15555,
						Action:      "quic_relay",
					},
				},
			},
		},
	}
	metadata := protocol.FlowMetadata{
		GameID:               "game",
		PolicyID:             "game-udp-v1",
		RuleID:               "game-udp-cidr",
		Network:              "udp",
		ClientConfigRevision: "r1",
	}

	if _, err := Evaluate(cfg, &auth.Principal{UserID: "user-1"}, metadata, "udp", "103.201.131.99", 15555); err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
}

func TestEvaluateDisabledWhenNoPolicies(t *testing.T) {
	if _, err := Evaluate(config.RoutePoliciesConfig{}, &auth.Principal{}, protocol.FlowMetadata{}, "tcp", "example.com", 443); err != nil {
		t.Fatalf("Evaluate returned error with empty config: %v", err)
	}
}

func testPolicies() config.RoutePoliciesConfig {
	allowTCP := true
	allowUDP := false
	return config.RoutePoliciesConfig{
		Revision: "20260616.1",
		Policies: []config.RoutePolicyConfig{
			{
				PolicyID: "steam-web-v1",
				GameID:   "steam",
				AllowTCP: &allowTCP,
				AllowUDP: &allowUDP,
				Rules: []config.RoutePolicyRuleConfig{
					{
						RuleID:      "steam-store-tcp-443",
						Network:     "tcp",
						TargetType:  "domain",
						TargetValue: "store.steampowered.com",
						PortStart:   443,
						PortEnd:     443,
						Action:      "quic_relay",
					},
				},
			},
		},
	}
}
