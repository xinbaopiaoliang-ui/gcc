package routepolicy

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"gaccel-node/internal/auth"
	"gaccel-node/internal/config"
	"gaccel-node/internal/protocol"
)

var ErrPolicyDenied = errors.New("route policy denied")

const (
	ModeStrict         = config.RoutePoliciesModeStrict
	ModeClientDecision = config.RoutePoliciesModeClientDecision
)

type Match struct {
	Revision string
	GameID   string
	PolicyID string
	RuleID   string
	Action   string
}

func Enabled(cfg config.RoutePoliciesConfig) bool {
	return mode(cfg) == ModeClientDecision || len(cfg.Policies) > 0
}

func Evaluate(cfg config.RoutePoliciesConfig, principal *auth.Principal, metadata protocol.FlowMetadata, network, targetHost string, targetPort int) (Match, error) {
	if !Enabled(cfg) {
		return Match{}, nil
	}
	network = strings.ToLower(strings.TrimSpace(network))
	if mode(cfg) == ModeClientDecision {
		return evaluateClientDecision(cfg, principal, metadata, network)
	}
	if err := metadata.ValidateForNetwork(network); err != nil {
		return Match{}, deny(err)
	}
	if principal == nil {
		return Match{}, deny(errors.New("principal is required"))
	}
	if !containsOrEmpty(principal.GameIDs, metadata.GameID) {
		return Match{}, deny(fmt.Errorf("token does not allow game_id %q", metadata.GameID))
	}
	if !containsOrEmpty(principal.PolicyIDs, metadata.PolicyID) {
		return Match{}, deny(fmt.Errorf("token does not allow policy_id %q", metadata.PolicyID))
	}
	if principal.ConfigRevision != "" && metadata.ClientConfigRevision != principal.ConfigRevision {
		return Match{}, deny(fmt.Errorf("metadata.client_config_revision %q does not match token config_revision %q", metadata.ClientConfigRevision, principal.ConfigRevision))
	}

	policy, ok := findPolicy(cfg, metadata.PolicyID)
	if !ok {
		return Match{}, deny(fmt.Errorf("policy_id %q is not configured on node", metadata.PolicyID))
	}
	if strings.TrimSpace(policy.GameID) != metadata.GameID {
		return Match{}, deny(fmt.Errorf("policy %q belongs to game_id %q, got %q", metadata.PolicyID, policy.GameID, metadata.GameID))
	}
	if network == "tcp" && policy.AllowTCP != nil && !*policy.AllowTCP {
		return Match{}, deny(fmt.Errorf("policy %q does not allow tcp", metadata.PolicyID))
	}
	if network == "udp" && policy.AllowUDP != nil && !*policy.AllowUDP {
		return Match{}, deny(fmt.Errorf("policy %q does not allow udp", metadata.PolicyID))
	}

	rule, ok := findRule(policy, metadata.RuleID)
	if !ok {
		return Match{}, deny(fmt.Errorf("rule_id %q is not configured in policy %q", metadata.RuleID, metadata.PolicyID))
	}
	if rule.Enabled != nil && !*rule.Enabled {
		return Match{}, deny(fmt.Errorf("rule_id %q is disabled", metadata.RuleID))
	}
	if !ruleNetworkMatches(rule.Network, network) {
		return Match{}, deny(fmt.Errorf("rule_id %q network %q does not match %q", metadata.RuleID, rule.Network, network))
	}
	if targetPort < rule.PortStart || targetPort > rule.PortEnd {
		return Match{}, deny(fmt.Errorf("target port %d is outside rule %q range %d-%d", targetPort, metadata.RuleID, rule.PortStart, rule.PortEnd))
	}
	if !targetMatches(rule.TargetType, rule.TargetValue, targetHost) {
		return Match{}, deny(fmt.Errorf("target %q does not match rule_id %q", targetHost, metadata.RuleID))
	}

	action := strings.ToLower(strings.TrimSpace(rule.Action))
	if action == "" {
		action = "quic_relay"
	}
	if action != "quic_relay" {
		return Match{}, deny(fmt.Errorf("rule_id %q action %q is not relayable by node", metadata.RuleID, action))
	}
	return Match{
		Revision: strings.TrimSpace(cfg.Revision),
		GameID:   metadata.GameID,
		PolicyID: metadata.PolicyID,
		RuleID:   metadata.RuleID,
		Action:   action,
	}, nil
}

func evaluateClientDecision(cfg config.RoutePoliciesConfig, principal *auth.Principal, metadata protocol.FlowMetadata, network string) (Match, error) {
	if principal == nil {
		return Match{}, deny(errors.New("principal is required"))
	}
	if metadata.Empty() && !principalRequiresMetadata(principal) {
		return Match{
			Revision: strings.TrimSpace(cfg.Revision),
			Action:   "quic_relay",
		}, nil
	}
	if err := metadata.ValidateClientDecisionForNetwork(network); err != nil {
		return Match{}, deny(err)
	}
	if !containsOrEmpty(principal.GameIDs, metadata.GameID) {
		return Match{}, deny(fmt.Errorf("token does not allow game_id %q", metadata.GameID))
	}
	if !containsOrEmpty(principal.PolicyIDs, metadata.PolicyID) {
		return Match{}, deny(fmt.Errorf("token does not allow policy_id %q", metadata.PolicyID))
	}
	if principal.ConfigRevision != "" && metadata.ClientConfigRevision != principal.ConfigRevision {
		return Match{}, deny(fmt.Errorf("metadata.client_config_revision %q does not match token config_revision %q", metadata.ClientConfigRevision, principal.ConfigRevision))
	}
	return Match{
		Revision: strings.TrimSpace(cfg.Revision),
		GameID:   metadata.GameID,
		PolicyID: metadata.PolicyID,
		RuleID:   metadata.RuleID,
		Action:   "quic_relay",
	}, nil
}

func mode(cfg config.RoutePoliciesConfig) string {
	return config.EffectiveRoutePoliciesMode(cfg)
}

func principalRequiresMetadata(principal *auth.Principal) bool {
	return len(principal.GameIDs) > 0 || len(principal.PolicyIDs) > 0 || strings.TrimSpace(principal.ConfigRevision) != ""
}

func deny(err error) error {
	if err == nil {
		return ErrPolicyDenied
	}
	return fmt.Errorf("%w: %w", ErrPolicyDenied, err)
}

func findPolicy(cfg config.RoutePoliciesConfig, policyID string) (config.RoutePolicyConfig, bool) {
	for _, policy := range cfg.Policies {
		if strings.TrimSpace(policy.PolicyID) == policyID {
			return policy, true
		}
	}
	return config.RoutePolicyConfig{}, false
}

func findRule(policy config.RoutePolicyConfig, ruleID string) (config.RoutePolicyRuleConfig, bool) {
	for _, rule := range policy.Rules {
		if strings.TrimSpace(rule.RuleID) == ruleID {
			return rule, true
		}
	}
	return config.RoutePolicyRuleConfig{}, false
}

func containsOrEmpty(values []string, value string) bool {
	if len(values) == 0 {
		return true
	}
	for _, item := range values {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

func ruleNetworkMatches(ruleNetwork, network string) bool {
	ruleNetwork = strings.ToLower(strings.TrimSpace(ruleNetwork))
	return ruleNetwork == "any" || ruleNetwork == network
}

func targetMatches(targetType, targetValue, targetHost string) bool {
	targetType = strings.ToLower(strings.TrimSpace(targetType))
	targetValue = normalizeHost(targetValue)
	targetHost = normalizeHost(targetHost)
	switch targetType {
	case "any":
		return true
	case "domain":
		return targetHost == targetValue
	case "domain_suffix":
		suffix := targetValue
		if !strings.HasPrefix(suffix, ".") {
			suffix = "." + suffix
		}
		return targetHost == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(targetHost, suffix)
	case "ip":
		want, err := netip.ParseAddr(targetValue)
		if err != nil {
			return false
		}
		got, err := netip.ParseAddr(targetHost)
		return err == nil && got == want
	case "cidr":
		prefix, err := netip.ParsePrefix(targetValue)
		if err != nil {
			return false
		}
		got, err := netip.ParseAddr(targetHost)
		return err == nil && prefix.Contains(got)
	default:
		return false
	}
}

func normalizeHost(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
