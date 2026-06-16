package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Node     NodeConfig     `yaml:"node"`
	Auth     AuthConfig     `yaml:"auth"`
	Limits   LimitsConfig   `yaml:"limits"`
	Security SecurityConfig `yaml:"security"`
	Panel    PanelConfig    `yaml:"panel"`
	Admin    AdminConfig    `yaml:"admin"`
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
	MaxQUICConnections int           `yaml:"max_quic_connections"`
	MaxUserConnections int           `yaml:"max_user_connections"`
	MaxFlowsPerConn    int           `yaml:"max_flows_per_conn"`
	QUICIdleTimeout    time.Duration `yaml:"quic_idle_timeout"`
	UDPIdleTimeout     time.Duration `yaml:"udp_idle_timeout"`
	TCPIdleTimeout     time.Duration `yaml:"tcp_idle_timeout"`
	UserRateLimitMbps  int           `yaml:"user_rate_limit_mbps"`
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

type PanelConfig struct {
	ReportURL string        `yaml:"report_url"`
	APIKey    string        `yaml:"api_key"`
	Interval  time.Duration `yaml:"interval"`
	Timeout   time.Duration `yaml:"timeout"`
}

type AdminConfig struct {
	Listen string `yaml:"listen"`
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
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
			MaxQUICConnections: 50000,
			MaxUserConnections: 8,
			MaxFlowsPerConn:    256,
			QUICIdleTimeout:    60 * time.Second,
			UDPIdleTimeout:     60 * time.Second,
			TCPIdleTimeout:     10 * time.Minute,
			UserRateLimitMbps:  100,
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
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
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
	cfg.Panel.APIKey = strings.TrimSpace(cfg.Panel.APIKey)
	if cfg.Panel.Interval == 0 {
		cfg.Panel.Interval = def.Panel.Interval
	}
	if cfg.Panel.Timeout == 0 {
		cfg.Panel.Timeout = def.Panel.Timeout
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
	return nil
}

func validatePanel(panel PanelConfig) error {
	if panel.Interval < 0 {
		return errors.New("panel.interval must be >= 0")
	}
	if panel.Timeout < 0 {
		return errors.New("panel.timeout must be >= 0")
	}
	if panel.ReportURL == "" {
		return nil
	}
	if panel.APIKey == "" {
		return errors.New("panel.api_key is required when panel.report_url is set")
	}
	parsed, err := url.Parse(panel.ReportURL)
	if err != nil {
		return fmt.Errorf("panel.report_url is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("panel.report_url must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("panel.report_url host is required")
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
