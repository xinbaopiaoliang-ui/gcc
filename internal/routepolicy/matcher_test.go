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
		Mode:     ModeStrict,
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

func TestEvaluateClientDecisionAllowsClientSelectedTarget(t *testing.T) {
	cfg := config.RoutePoliciesConfig{
		Mode:     ModeClientDecision,
		Revision: "delta-force-20260701",
	}
	principal := &auth.Principal{
		UserID:         "user-1",
		GameIDs:        []string{"2064165410042941440"},
		PolicyIDs:      []string{"2072129373590392832"},
		ConfigRevision: "delta-force-20260701",
	}
	metadata := protocol.FlowMetadata{
		GameID:               "2064165410042941440",
		PolicyID:             "2072129373590392832",
		Network:              "tcp",
		ClientConfigRevision: "delta-force-20260701",
	}

	match, err := Evaluate(cfg, principal, metadata, "tcp", "dir.200061208-2-1.dmpplat.com", 8085)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if match.PolicyID != "2072129373590392832" || match.GameID != "2064165410042941440" || match.Action != "quic_relay" {
		t.Fatalf("match = %#v", match)
	}
}

func TestEvaluateClientDecisionDefaultForPolicyBundles(t *testing.T) {
	cfg := config.RoutePoliciesConfig{
		Revision: "delta-force-20260701",
		Policies: []config.RoutePolicyConfig{
			{
				PolicyID: "2072129373590392832",
				GameID:   "2064165410042941440",
				Rules: []config.RoutePolicyRuleConfig{
					{
						RuleID:      "legacy-single-rule",
						Network:     "tcp",
						TargetType:  "domain",
						TargetValue: "example.invalid",
						PortStart:   443,
						PortEnd:     443,
						Action:      "quic_relay",
					},
				},
			},
		},
	}
	principal := &auth.Principal{
		UserID:         "user-1",
		GameIDs:        []string{"2064165410042941440"},
		PolicyIDs:      []string{"2072129373590392832"},
		ConfigRevision: "delta-force-20260701",
	}
	metadata := protocol.FlowMetadata{
		GameID:               "2064165410042941440",
		PolicyID:             "2072129373590392832",
		Network:              "tcp",
		ClientConfigRevision: "delta-force-20260701",
	}

	if _, err := Evaluate(cfg, principal, metadata, "tcp", "dir.200061208-2-1.dmpplat.com", 8085); err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
}

func TestEvaluateClientDecisionStillRequiresTokenPolicy(t *testing.T) {
	cfg := config.RoutePoliciesConfig{
		Mode:     ModeClientDecision,
		Revision: "delta-force-20260701",
	}
	principal := &auth.Principal{
		UserID:    "user-1",
		GameIDs:   []string{"2064165410042941440"},
		PolicyIDs: []string{"other-policy"},
	}
	metadata := protocol.FlowMetadata{
		GameID:               "2064165410042941440",
		PolicyID:             "2072129373590392832",
		Network:              "tcp",
		ClientConfigRevision: "delta-force-20260701",
	}

	if _, err := Evaluate(cfg, principal, metadata, "tcp", "dir.200061208-2-1.dmpplat.com", 8085); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Evaluate error = %v, want ErrPolicyDenied", err)
	}
}

func TestEvaluateClientDecisionAllowsLegacyProbeWithoutRestrictedClaims(t *testing.T) {
	cfg := config.RoutePoliciesConfig{
		Mode:     ModeClientDecision,
		Revision: "client-decision",
	}

	if _, err := Evaluate(cfg, &auth.Principal{UserID: "probe"}, protocol.FlowMetadata{}, "tcp", "example.com", 8085); err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
}

func TestEvaluateClientDecisionRequiresMetadataForRestrictedToken(t *testing.T) {
	cfg := config.RoutePoliciesConfig{
		Mode:     ModeClientDecision,
		Revision: "client-decision",
	}
	principal := &auth.Principal{UserID: "user-1", PolicyIDs: []string{"policy-1"}}

	if _, err := Evaluate(cfg, principal, protocol.FlowMetadata{}, "tcp", "example.com", 8085); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("Evaluate error = %v, want ErrPolicyDenied", err)
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
		Mode:     ModeStrict,
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
