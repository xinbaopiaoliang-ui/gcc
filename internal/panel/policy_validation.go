package panel

import (
	"sort"
	"strconv"
	"strings"

	"gaccel-node/internal/config"
)

type PolicyValidationRequest struct {
	Revision              string `json:"revision"`
	SHA256                string `json:"sha256,omitempty"`
	RoutePoliciesYAML     string `json:"route_policies_yaml"`
	BaseRevision          string `json:"base_revision,omitempty"`
	BaseRoutePoliciesYAML string `json:"base_route_policies_yaml,omitempty"`
}

type PolicyValidationResponse struct {
	Valid    bool                    `json:"valid"`
	SHA256   string                  `json:"sha256"`
	Errors   []string                `json:"errors"`
	Warnings []string                `json:"warnings"`
	Summary  PolicyValidationSummary `json:"summary"`
	Diff     *PolicyValidationDiff   `json:"diff,omitempty"`
}

type PolicyValidationSummary struct {
	Revision       string   `json:"revision"`
	PolicyCount    int      `json:"policy_count"`
	RuleCount      int      `json:"rule_count"`
	RelayRuleCount int      `json:"relay_rule_count"`
	DisabledRules  int      `json:"disabled_rules"`
	Games          []string `json:"games"`
	Policies       []string `json:"policies"`
	Networks       []string `json:"networks"`
	TargetTypes    []string `json:"target_types"`
}

type PolicyValidationDiff struct {
	BaseRevision      string   `json:"base_revision"`
	CandidateRevision string   `json:"candidate_revision"`
	AddedPolicies     []string `json:"added_policies"`
	RemovedPolicies   []string `json:"removed_policies"`
	ChangedPolicies   []string `json:"changed_policies"`
	AddedRules        []string `json:"added_rules"`
	RemovedRules      []string `json:"removed_rules"`
	ChangedRules      []string `json:"changed_rules"`
	LineDiff          []string `json:"line_diff"`
}

func ValidatePolicyPackage(req PolicyValidationRequest) PolicyValidationResponse {
	resp := PolicyValidationResponse{
		SHA256: sha256Hex([]byte(req.RoutePoliciesYAML)),
	}
	if strings.TrimSpace(req.RoutePoliciesYAML) == "" {
		resp.Errors = append(resp.Errors, "route_policies_yaml is required")
		return resp
	}
	if sha := normalizeSHA256(req.SHA256); sha != "" {
		if !isSHA256Hex(sha) {
			resp.Errors = append(resp.Errors, "sha256 must be 64 lowercase hex chars")
		} else if sha != resp.SHA256 {
			resp.Errors = append(resp.Errors, "sha256 does not match route_policies_yaml")
		}
	}
	candidate, err := config.ParseRoutePoliciesData([]byte(req.RoutePoliciesYAML))
	if err != nil {
		resp.Errors = append(resp.Errors, err.Error())
		return resp
	}
	resp.Summary = summarizeRoutePolicies(candidate)
	if revision := strings.TrimSpace(req.Revision); revision != "" && strings.TrimSpace(candidate.Revision) != "" && revision != strings.TrimSpace(candidate.Revision) {
		resp.Warnings = append(resp.Warnings, "request revision does not match route_policies.revision")
	}
	if strings.TrimSpace(candidate.Revision) == "" {
		resp.Warnings = append(resp.Warnings, "route_policies.revision is empty; node can apply it, but sync troubleshooting is harder")
	}
	if len(candidate.Policies) == 0 {
		resp.Warnings = append(resp.Warnings, "route_policies.policies is empty; node will not enforce per-game route policy matching")
	}
	if resp.Summary.RelayRuleCount == 0 && resp.Summary.RuleCount > 0 {
		resp.Warnings = append(resp.Warnings, "no quic_relay rules were found; block/direct rules are not relayable by the node")
	}
	if strings.TrimSpace(req.BaseRoutePoliciesYAML) != "" {
		if base, err := config.ParseRoutePoliciesData([]byte(req.BaseRoutePoliciesYAML)); err == nil {
			diff := diffRoutePolicies(base, candidate, req.BaseRoutePoliciesYAML, req.RoutePoliciesYAML)
			resp.Diff = &diff
		} else {
			resp.Warnings = append(resp.Warnings, "base policy could not be parsed for diff: "+err.Error())
		}
	}
	resp.Valid = len(resp.Errors) == 0
	return resp
}

func summarizeRoutePolicies(routePolicies config.RoutePoliciesConfig) PolicyValidationSummary {
	games := make(map[string]struct{})
	policies := make(map[string]struct{})
	networks := make(map[string]struct{})
	targetTypes := make(map[string]struct{})
	summary := PolicyValidationSummary{
		Revision: strings.TrimSpace(routePolicies.Revision),
	}
	for _, policy := range routePolicies.Policies {
		policyID := strings.TrimSpace(policy.PolicyID)
		gameID := strings.TrimSpace(policy.GameID)
		if policyID != "" {
			policies[policyID] = struct{}{}
		}
		if gameID != "" {
			games[gameID] = struct{}{}
		}
		summary.PolicyCount++
		for _, rule := range policy.Rules {
			summary.RuleCount++
			network := strings.ToLower(strings.TrimSpace(rule.Network))
			if network != "" {
				networks[network] = struct{}{}
			}
			targetType := strings.ToLower(strings.TrimSpace(rule.TargetType))
			if targetType != "" {
				targetTypes[targetType] = struct{}{}
			}
			if rule.Enabled != nil && !*rule.Enabled {
				summary.DisabledRules++
			}
			action := strings.ToLower(strings.TrimSpace(rule.Action))
			if action == "" || action == "quic_relay" {
				summary.RelayRuleCount++
			}
		}
	}
	summary.Games = sortedKeys(games)
	summary.Policies = sortedKeys(policies)
	summary.Networks = sortedKeys(networks)
	summary.TargetTypes = sortedKeys(targetTypes)
	return summary
}

func diffRoutePolicies(base, candidate config.RoutePoliciesConfig, baseYAML, candidateYAML string) PolicyValidationDiff {
	basePolicies := policyFingerprintMap(base)
	candidatePolicies := policyFingerprintMap(candidate)
	baseRules := ruleFingerprintMap(base)
	candidateRules := ruleFingerprintMap(candidate)
	return PolicyValidationDiff{
		BaseRevision:      strings.TrimSpace(base.Revision),
		CandidateRevision: strings.TrimSpace(candidate.Revision),
		AddedPolicies:     addedKeys(basePolicies, candidatePolicies),
		RemovedPolicies:   addedKeys(candidatePolicies, basePolicies),
		ChangedPolicies:   changedKeys(basePolicies, candidatePolicies),
		AddedRules:        addedKeys(baseRules, candidateRules),
		RemovedRules:      addedKeys(candidateRules, baseRules),
		ChangedRules:      changedKeys(baseRules, candidateRules),
		LineDiff:          simpleLineDiff(baseYAML, candidateYAML, 120),
	}
}

func policyFingerprintMap(routePolicies config.RoutePoliciesConfig) map[string]string {
	values := make(map[string]string, len(routePolicies.Policies))
	for _, policy := range routePolicies.Policies {
		key := strings.TrimSpace(policy.PolicyID)
		if key == "" {
			continue
		}
		values[key] = strings.Join([]string{
			strings.TrimSpace(policy.GameID),
			boolPointerFingerprint(policy.AllowTCP),
			boolPointerFingerprint(policy.AllowUDP),
			strings.Join(ruleIDs(policy.Rules), ","),
		}, "|")
	}
	return values
}

func ruleFingerprintMap(routePolicies config.RoutePoliciesConfig) map[string]string {
	values := make(map[string]string)
	for _, policy := range routePolicies.Policies {
		policyID := strings.TrimSpace(policy.PolicyID)
		for _, rule := range policy.Rules {
			ruleID := strings.TrimSpace(rule.RuleID)
			if ruleID == "" {
				continue
			}
			key := policyID + "/" + ruleID
			values[key] = strings.Join([]string{
				strings.ToLower(strings.TrimSpace(rule.Network)),
				strings.ToLower(strings.TrimSpace(rule.TargetType)),
				strings.TrimSpace(rule.TargetValue),
				intString(rule.PortStart),
				intString(rule.PortEnd),
				strings.ToLower(strings.TrimSpace(rule.Action)),
				boolPointerFingerprint(rule.Enabled),
			}, "|")
		}
	}
	return values
}

func ruleIDs(rules []config.RoutePolicyRuleConfig) []string {
	values := make([]string, 0, len(rules))
	for _, rule := range rules {
		if ruleID := strings.TrimSpace(rule.RuleID); ruleID != "" {
			values = append(values, ruleID)
		}
	}
	return sortStrings(values)
}

func boolPointerFingerprint(value *bool) string {
	if value == nil {
		return "-"
	}
	if *value {
		return "true"
	}
	return "false"
}

func intString(value int) string {
	return strconv.Itoa(value)
}

func addedKeys(base, candidate map[string]string) []string {
	values := make([]string, 0)
	for key := range candidate {
		if _, ok := base[key]; !ok {
			values = append(values, key)
		}
	}
	return sortStrings(values)
}

func changedKeys(base, candidate map[string]string) []string {
	values := make([]string, 0)
	for key, candidateValue := range candidate {
		if baseValue, ok := base[key]; ok && baseValue != candidateValue {
			values = append(values, key)
		}
	}
	return sortStrings(values)
}

func simpleLineDiff(baseYAML, candidateYAML string, maxLines int) []string {
	baseLines := strings.Split(normalizeTextLines(baseYAML), "\n")
	candidateLines := strings.Split(normalizeTextLines(candidateYAML), "\n")
	lines := make([]string, 0)
	max := len(baseLines)
	if len(candidateLines) > max {
		max = len(candidateLines)
	}
	for i := 0; i < max; i++ {
		var oldLine, newLine string
		if i < len(baseLines) {
			oldLine = baseLines[i]
		}
		if i < len(candidateLines) {
			newLine = candidateLines[i]
		}
		if oldLine == newLine {
			continue
		}
		if oldLine != "" {
			lines = append(lines, "- "+oldLine)
		}
		if newLine != "" {
			lines = append(lines, "+ "+newLine)
		}
		if maxLines > 0 && len(lines) >= maxLines {
			lines = append(lines, "... diff truncated")
			break
		}
	}
	return lines
}

func normalizeTextLines(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return sortStrings(keys)
}

func sortStrings(values []string) []string {
	sort.Strings(values)
	return values
}
