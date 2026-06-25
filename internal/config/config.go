package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Node          NodeConfig          `yaml:"node"`
	Auth          AuthConfig          `yaml:"auth"`
	Limits        LimitsConfig        `yaml:"limits"`
	Security      SecurityConfig      `yaml:"security"`
	RoutePolicies RoutePoliciesConfig `yaml:"route_policies" json:"route_policies"`
	Panel         PanelConfig         `yaml:"panel"`
	Upgrade       UpgradeConfig       `yaml:"upgrade"`
	Admin         AdminConfig         `yaml:"admin"`
}

type ServerConfig struct {
	Listen   string `yaml:"listen"`
	ALPN     string `yaml:"alpn"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type NodeConfig struct {
	ID     string            `yaml:"id" json:"id"`
	Region string            `yaml:"region" json:"region"`
	Tags   []string          `yaml:"tags" json:"tags"`
	Labels map[string]string `yaml:"labels" json:"labels"`
}

type AuthConfig struct {
	Mode        string        `yaml:"mode"`
	DevTokens   []string      `yaml:"dev_tokens"`
	HMACSecret  string        `yaml:"hmac_secret"`
	TokenLeeway time.Duration `yaml:"token_leeway"`
}

type LimitsConfig struct {
	MaxQUICConnections       int           `yaml:"max_quic_connections"`
	MaxUserConnections       int           `yaml:"max_user_connections"`
	MaxFlowsPerConn          int           `yaml:"max_flows_per_conn"`
	HeartbeatInterval        time.Duration `yaml:"heartbeat_interval"`
	SessionDisconnectTimeout time.Duration `yaml:"session_disconnect_timeout"`
	QUICIdleTimeout          time.Duration `yaml:"quic_idle_timeout"`
	UDPIdleTimeout           time.Duration `yaml:"udp_idle_timeout"`
	TCPIdleTimeout           time.Duration `yaml:"tcp_idle_timeout"`
	UserRateLimitMbps        int           `yaml:"user_rate_limit_mbps"`
}

type SecurityConfig struct {
	DenyPrivateIP     bool     `yaml:"deny_private_ip"`
	DenyLoopback      bool     `yaml:"deny_loopback"`
	DenyLinkLocal     bool     `yaml:"deny_link_local"`
	DenyMulticast     bool     `yaml:"deny_multicast"`
	DenyCloudMetadata bool     `yaml:"deny_cloud_metadata"`
	AllowedUDPPorts   []string `yaml:"allowed_udp_ports"`
	AllowedTCPPorts   []string `yaml:"allowed_tcp_ports"`
	BlockedTCPPorts   []string `yaml:"blocked_tcp_ports"`
	BlockedUDPPorts   []string `yaml:"blocked_udp_ports"`
}

type RoutePoliciesConfig struct {
	Revision string              `yaml:"revision" json:"revision"`
	Policies []RoutePolicyConfig `yaml:"policies" json:"policies"`
}

type RoutePolicyConfig struct {
	PolicyID string                  `yaml:"policy_id" json:"policy_id"`
	GameID   string                  `yaml:"game_id" json:"game_id"`
	Name     string                  `yaml:"name,omitempty" json:"name,omitempty"`
	AllowTCP *bool                   `yaml:"allow_tcp,omitempty" json:"allow_tcp,omitempty"`
	AllowUDP *bool                   `yaml:"allow_udp,omitempty" json:"allow_udp,omitempty"`
	Rules    []RoutePolicyRuleConfig `yaml:"rules" json:"rules"`
}

type RoutePolicyRuleConfig struct {
	RuleID      string `yaml:"rule_id" json:"rule_id"`
	Network     string `yaml:"network" json:"network"`
	TargetType  string `yaml:"target_type" json:"target_type"`
	TargetValue string `yaml:"target_value" json:"target_value"`
	PortStart   int    `yaml:"port_start" json:"port_start"`
	PortEnd     int    `yaml:"port_end" json:"port_end"`
	Action      string `yaml:"action" json:"action"`
	Priority    int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Enabled     *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type PanelConfig struct {
	ReportURL           string        `yaml:"report_url"`
	CommandURL          string        `yaml:"command_url"`
	APIKey              string        `yaml:"api_key"`
	CommandSecret       string        `yaml:"command_secret"`
	Interval            time.Duration `yaml:"interval"`
	Timeout             time.Duration `yaml:"timeout"`
	CommandInterval     time.Duration `yaml:"command_interval"`
	CommandTimeout      time.Duration `yaml:"command_timeout"`
	CommandMaxClockSkew time.Duration `yaml:"command_max_clock_skew"`
}

type UpgradeConfig struct {
	StageDir        string        `yaml:"stage_dir"`
	MaxPackageBytes int64         `yaml:"max_package_bytes"`
	Timeout         time.Duration `yaml:"timeout"`
	AllowHTTP       bool          `yaml:"allow_http"`
}

type AdminConfig struct {
	Listen string `yaml:"listen"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadData(data)
}

func LoadData(data []byte) (*Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyDefaults(cfg)
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Listen: ":443",
			ALPN:   "gaccel/1",
		},
		Auth: AuthConfig{
			Mode:        "dev",
			DevTokens:   []string{"dev-token"},
			TokenLeeway: 30 * time.Second,
		},
		Limits: LimitsConfig{
			MaxQUICConnections:       50000,
			MaxUserConnections:       8,
			MaxFlowsPerConn:          256,
			HeartbeatInterval:        15 * time.Second,
			SessionDisconnectTimeout: 45 * time.Second,
			QUICIdleTimeout:          60 * time.Second,
			UDPIdleTimeout:           60 * time.Second,
			TCPIdleTimeout:           10 * time.Minute,
			UserRateLimitMbps:        100,
		},
		Security: SecurityConfig{
			DenyPrivateIP:     true,
			DenyLoopback:      true,
			DenyLinkLocal:     true,
			DenyMulticast:     true,
			DenyCloudMetadata: true,
			AllowedUDPPorts:   []string{"1-65535"},
			AllowedTCPPorts:   []string{"80", "443", "1935", "5222", "27000-65535"},
			BlockedTCPPorts:   []string{"22", "25", "3306", "5432", "6379"},
		},
		Panel: PanelConfig{
			Interval:            30 * time.Second,
			Timeout:             10 * time.Second,
			CommandInterval:     30 * time.Second,
			CommandTimeout:      10 * time.Second,
			CommandMaxClockSkew: 2 * time.Minute,
		},
		Upgrade: UpgradeConfig{
			StageDir:        "/var/lib/gaccel-node/upgrades",
			MaxPackageBytes: 200 * 1024 * 1024,
			Timeout:         2 * time.Minute,
		},
		Admin: AdminConfig{
			Listen: "127.0.0.1:9090",
		},
	}
}

func applyDefaults(cfg *Config) {
	def := Default()
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = def.Server.Listen
	}
	if cfg.Server.ALPN == "" {
		cfg.Server.ALPN = def.Server.ALPN
	}
	if cfg.Auth.Mode == "" {
		cfg.Auth.Mode = def.Auth.Mode
	}
	if len(cfg.Auth.DevTokens) == 0 && cfg.Auth.Mode == "dev" {
		cfg.Auth.DevTokens = def.Auth.DevTokens
	}
	if cfg.Auth.TokenLeeway == 0 {
		cfg.Auth.TokenLeeway = def.Auth.TokenLeeway
	}
	if cfg.Limits.MaxQUICConnections == 0 {
		cfg.Limits.MaxQUICConnections = def.Limits.MaxQUICConnections
	}
	if cfg.Limits.MaxUserConnections == 0 {
		cfg.Limits.MaxUserConnections = def.Limits.MaxUserConnections
	}
	if cfg.Limits.MaxFlowsPerConn == 0 {
		cfg.Limits.MaxFlowsPerConn = def.Limits.MaxFlowsPerConn
	}
	if cfg.Limits.HeartbeatInterval == 0 {
		cfg.Limits.HeartbeatInterval = def.Limits.HeartbeatInterval
	}
	if cfg.Limits.SessionDisconnectTimeout == 0 {
		cfg.Limits.SessionDisconnectTimeout = def.Limits.SessionDisconnectTimeout
	}
	if cfg.Limits.QUICIdleTimeout == 0 {
		cfg.Limits.QUICIdleTimeout = def.Limits.QUICIdleTimeout
	}
	if cfg.Limits.UDPIdleTimeout == 0 {
		cfg.Limits.UDPIdleTimeout = def.Limits.UDPIdleTimeout
	}
	if cfg.Limits.TCPIdleTimeout == 0 {
		cfg.Limits.TCPIdleTimeout = def.Limits.TCPIdleTimeout
	}
	if cfg.Admin.Listen == "" {
		cfg.Admin.Listen = def.Admin.Listen
	}
	if len(cfg.Security.AllowedUDPPorts) == 0 {
		cfg.Security.AllowedUDPPorts = def.Security.AllowedUDPPorts
	}
	if len(cfg.Security.AllowedTCPPorts) == 0 {
		cfg.Security.AllowedTCPPorts = def.Security.AllowedTCPPorts
	}
	cfg.Panel.ReportURL = strings.TrimSpace(cfg.Panel.ReportURL)
	cfg.Panel.CommandURL = strings.TrimSpace(cfg.Panel.CommandURL)
	cfg.Panel.APIKey = strings.TrimSpace(cfg.Panel.APIKey)
	cfg.Panel.CommandSecret = strings.TrimSpace(cfg.Panel.CommandSecret)
	if cfg.Panel.Interval == 0 {
		cfg.Panel.Interval = def.Panel.Interval
	}
	if cfg.Panel.Timeout == 0 {
		cfg.Panel.Timeout = def.Panel.Timeout
	}
	if cfg.Panel.CommandInterval == 0 {
		cfg.Panel.CommandInterval = def.Panel.CommandInterval
	}
	if cfg.Panel.CommandTimeout == 0 {
		cfg.Panel.CommandTimeout = def.Panel.CommandTimeout
	}
	if cfg.Panel.CommandMaxClockSkew == 0 {
		cfg.Panel.CommandMaxClockSkew = def.Panel.CommandMaxClockSkew
	}
	cfg.Upgrade.StageDir = strings.TrimSpace(cfg.Upgrade.StageDir)
	if cfg.Upgrade.StageDir == "" {
		cfg.Upgrade.StageDir = def.Upgrade.StageDir
	}
	if cfg.Upgrade.MaxPackageBytes == 0 {
		cfg.Upgrade.MaxPackageBytes = def.Upgrade.MaxPackageBytes
	}
	if cfg.Upgrade.Timeout == 0 {
		cfg.Upgrade.Timeout = def.Upgrade.Timeout
	}
	normalizeNode(&cfg.Node)
}

func validate(cfg *Config) error {
	if cfg.Server.Listen == "" {
		return errors.New("server.listen is required")
	}
	if cfg.Server.ALPN == "" {
		return errors.New("server.alpn is required")
	}
	if cfg.Server.CertFile == "" || cfg.Server.KeyFile == "" {
		return fmt.Errorf("server.cert_file and server.key_file are required")
	}
	if cfg.Auth.TokenLeeway < 0 {
		return errors.New("auth.token_leeway must be >= 0")
	}
	switch cfg.Auth.Mode {
	case "dev":
		if len(cfg.Auth.DevTokens) == 0 {
			return errors.New("auth.dev_tokens must not be empty in dev mode")
		}
	case "hmac":
		if cfg.Auth.HMACSecret == "" {
			return errors.New("auth.hmac_secret is required in hmac mode")
		}
	default:
		return fmt.Errorf("unsupported auth.mode %q", cfg.Auth.Mode)
	}
	if err := validateNode(cfg.Node); err != nil {
		return err
	}
	if err := validatePanel(cfg.Panel); err != nil {
		return err
	}
	if err := validateUpgrade(cfg.Upgrade); err != nil {
		return err
	}
	if err := validateRoutePolicies(cfg.RoutePolicies); err != nil {
		return err
	}
	return nil
}

func validateUpgrade(upgrade UpgradeConfig) error {
	if upgrade.StageDir == "" {
		return errors.New("upgrade.stage_dir is required")
	}
	if upgrade.MaxPackageBytes < 0 {
		return errors.New("upgrade.max_package_bytes must be >= 0")
	}
	if upgrade.Timeout < 0 {
		return errors.New("upgrade.timeout must be >= 0")
	}
	return nil
}

func validatePanel(panel PanelConfig) error {
	if panel.Interval < 0 {
		return errors.New("panel.interval must be >= 0")
	}
	if panel.Timeout < 0 {
		return errors.New("panel.timeout must be >= 0")
	}
	if panel.CommandInterval < 0 {
		return errors.New("panel.command_interval must be >= 0")
	}
	if panel.CommandTimeout < 0 {
		return errors.New("panel.command_timeout must be >= 0")
	}
	if panel.CommandMaxClockSkew < 0 {
		return errors.New("panel.command_max_clock_skew must be >= 0")
	}
	if (panel.ReportURL != "" || panel.CommandURL != "") && panel.APIKey == "" {
		return errors.New("panel.api_key is required when panel.report_url or panel.command_url is set")
	}
	if panel.CommandURL != "" && panel.CommandSecret == "" {
		return errors.New("panel.command_secret is required when panel.command_url is set")
	}
	if err := validatePanelURL("panel.report_url", panel.ReportURL); err != nil {
		return err
	}
	if err := validatePanelURL("panel.command_url", panel.CommandURL); err != nil {
		return err
	}
	return nil
}

func validatePanelURL(name, value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", name)
	}
	return nil
}

func normalizeNode(node *NodeConfig) {
	node.ID = strings.TrimSpace(node.ID)
	node.Region = strings.TrimSpace(node.Region)
	node.Tags = cleanStringList(node.Tags)
	if node.Labels == nil {
		node.Labels = map[string]string{}
		return
	}
	labels := make(map[string]string, len(node.Labels))
	for key, value := range node.Labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			labels[key] = value
		}
	}
	node.Labels = labels
}

func validateRoutePolicies(routePolicies RoutePoliciesConfig) error {
	policies := make(map[string]struct{}, len(routePolicies.Policies))
	rules := make(map[string]struct{})
	for i, policy := range routePolicies.Policies {
		policyID := strings.TrimSpace(policy.PolicyID)
		gameID := strings.TrimSpace(policy.GameID)
		if policyID == "" {
			return fmt.Errorf("route_policies.policies[%d].policy_id is required", i)
		}
		if gameID == "" {
			return fmt.Errorf("route_policies.policies[%d].game_id is required", i)
		}
		if _, ok := policies[policyID]; ok {
			return fmt.Errorf("route_policies duplicate policy_id %q", policyID)
		}
		policies[policyID] = struct{}{}
		for j, rule := range policy.Rules {
			if err := validateRoutePolicyRule(policyID, j, rule); err != nil {
				return err
			}
			ruleID := strings.TrimSpace(rule.RuleID)
			if _, ok := rules[ruleID]; ok {
				return fmt.Errorf("route_policies duplicate rule_id %q", ruleID)
			}
			rules[ruleID] = struct{}{}
		}
	}
	return nil
}

func validateRoutePolicyRule(policyID string, index int, rule RoutePolicyRuleConfig) error {
	prefix := fmt.Sprintf("route_policies policy %q rule[%d]", policyID, index)
	if strings.TrimSpace(rule.RuleID) == "" {
		return fmt.Errorf("%s rule_id is required", prefix)
	}
	switch strings.ToLower(strings.TrimSpace(rule.Network)) {
	case "tcp", "udp", "any":
	default:
		return fmt.Errorf("%s network must be tcp, udp, or any", prefix)
	}
	switch strings.ToLower(strings.TrimSpace(rule.TargetType)) {
	case "domain", "domain_suffix", "ip", "cidr", "any":
	default:
		return fmt.Errorf("%s target_type must be domain, domain_suffix, ip, cidr, or any", prefix)
	}
	targetValue := strings.TrimSpace(rule.TargetValue)
	if targetValue == "" && strings.ToLower(strings.TrimSpace(rule.TargetType)) != "any" {
		return fmt.Errorf("%s target_value is required", prefix)
	}
	if strings.ToLower(strings.TrimSpace(rule.TargetType)) == "cidr" {
		if _, err := netip.ParsePrefix(targetValue); err != nil {
			return fmt.Errorf("%s target_value must be a CIDR prefix: %w", prefix, err)
		}
	}
	if strings.ToLower(strings.TrimSpace(rule.TargetType)) == "ip" {
		if _, err := netip.ParseAddr(targetValue); err != nil {
			return fmt.Errorf("%s target_value must be an IP address: %w", prefix, err)
		}
	}
	if rule.PortStart < 1 || rule.PortStart > 65535 {
		return fmt.Errorf("%s port_start must be between 1 and 65535", prefix)
	}
	if rule.PortEnd < 1 || rule.PortEnd > 65535 {
		return fmt.Errorf("%s port_end must be between 1 and 65535", prefix)
	}
	if rule.PortEnd < rule.PortStart {
		return fmt.Errorf("%s port_end must be >= port_start", prefix)
	}
	switch strings.ToLower(strings.TrimSpace(rule.Action)) {
	case "", "quic_relay", "block", "direct":
	default:
		return fmt.Errorf("%s action must be quic_relay, block, or direct", prefix)
	}
	return nil
}

func validateNode(node NodeConfig) error {
	for key := range node.Labels {
		if strings.ContainsAny(key, "\r\n\t") {
			return fmt.Errorf("node.labels contains invalid key %q", key)
		}
	}
	for _, tag := range node.Tags {
		if strings.ContainsAny(tag, "\r\n\t") {
			return fmt.Errorf("node.tags contains invalid value %q", tag)
		}
	}
	return nil
}

func cleanStringList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
