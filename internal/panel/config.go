package panel

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen        string         `yaml:"listen"`
	PublicBaseURL string         `yaml:"public_base_url"`
	Database      DatabaseConfig `yaml:"database"`
	Security      SecurityConfig `yaml:"security"`
	Session       SessionConfig  `yaml:"session"`
	NodeCommand   CommandConfig  `yaml:"node_command"`
	Deploy        DeployConfig   `yaml:"deploy"`
	CORS          CORSConfig     `yaml:"cors"`
	Web           WebConfig      `yaml:"web"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type SecurityConfig struct {
	MasterKey      string   `yaml:"master_key"`
	BackendAPIKeys []string `yaml:"backend_api_keys"`
}

type SessionConfig struct {
	CookieName string        `yaml:"cookie_name"`
	Secret     string        `yaml:"secret"`
	TTL        time.Duration `yaml:"ttl"`
}

type CommandConfig struct {
	Secret           string        `yaml:"secret"`
	MaxClockSkew     time.Duration `yaml:"max_clock_skew"`
	NonceTTL         time.Duration `yaml:"nonce_ttl"`
	DefaultExpiresIn time.Duration `yaml:"default_expires_in"`
}

type DeployConfig struct {
	DefaultNodeVersion string        `yaml:"default_node_version"`
	SSHTimeout         time.Duration `yaml:"ssh_timeout"`
	CommandTimeout     time.Duration `yaml:"command_timeout"`
}

type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type WebConfig struct {
	Root string `yaml:"root"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyEnv(cfg)
	applyDefaults(cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		Listen: "127.0.0.1:8090",
		Database: DatabaseConfig{
			Driver: "mysql",
		},
		Session: SessionConfig{
			CookieName: "gaccel_panel_session",
			TTL:        12 * time.Hour,
		},
		NodeCommand: CommandConfig{
			MaxClockSkew:     2 * time.Minute,
			NonceTTL:         2 * time.Minute,
			DefaultExpiresIn: 2 * time.Minute,
		},
		Deploy: DeployConfig{
			DefaultNodeVersion: "latest",
			SSHTimeout:         15 * time.Second,
			CommandTimeout:     2 * time.Minute,
		},
	}
}

func applyEnv(cfg *Config) {
	if value := os.Getenv("GACCEL_PANEL_DATABASE_DSN"); value != "" {
		cfg.Database.DSN = value
	}
	if value := os.Getenv("GACCEL_PANEL_MASTER_KEY"); value != "" {
		cfg.Security.MasterKey = value
	}
	if value := os.Getenv("GACCEL_PANEL_BACKEND_API_KEYS"); value != "" {
		cfg.Security.BackendAPIKeys = splitCSV(value)
	}
	if value := os.Getenv("GACCEL_PANEL_SESSION_SECRET"); value != "" {
		cfg.Session.Secret = value
	}
	if value := os.Getenv("GACCEL_PANEL_COMMAND_SECRET"); value != "" {
		cfg.NodeCommand.Secret = value
	}
	if value := os.Getenv("GACCEL_PANEL_CORS_ALLOWED_ORIGINS"); value != "" {
		cfg.CORS.AllowedOrigins = splitCSV(value)
	}
	if value := os.Getenv("GACCEL_PANEL_WEB_ROOT"); value != "" {
		cfg.Web.Root = value
	}
}

func applyDefaults(cfg *Config) {
	def := DefaultConfig()
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	if cfg.Listen == "" {
		cfg.Listen = def.Listen
	}
	cfg.PublicBaseURL = strings.TrimSpace(cfg.PublicBaseURL)
	cfg.Database.Driver = strings.TrimSpace(cfg.Database.Driver)
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = def.Database.Driver
	}
	cfg.Database.DSN = strings.TrimSpace(cfg.Database.DSN)
	cfg.Security.MasterKey = strings.TrimSpace(cfg.Security.MasterKey)
	cfg.Security.BackendAPIKeys = cleanList(cfg.Security.BackendAPIKeys)
	cfg.Session.CookieName = strings.TrimSpace(cfg.Session.CookieName)
	if cfg.Session.CookieName == "" {
		cfg.Session.CookieName = def.Session.CookieName
	}
	cfg.Session.Secret = strings.TrimSpace(cfg.Session.Secret)
	if cfg.Session.TTL == 0 {
		cfg.Session.TTL = def.Session.TTL
	}
	cfg.NodeCommand.Secret = strings.TrimSpace(cfg.NodeCommand.Secret)
	if cfg.NodeCommand.MaxClockSkew == 0 {
		cfg.NodeCommand.MaxClockSkew = def.NodeCommand.MaxClockSkew
	}
	if cfg.NodeCommand.NonceTTL == 0 {
		cfg.NodeCommand.NonceTTL = def.NodeCommand.NonceTTL
	}
	if cfg.NodeCommand.DefaultExpiresIn == 0 {
		cfg.NodeCommand.DefaultExpiresIn = def.NodeCommand.DefaultExpiresIn
	}
	cfg.Deploy.DefaultNodeVersion = strings.TrimSpace(cfg.Deploy.DefaultNodeVersion)
	if cfg.Deploy.DefaultNodeVersion == "" {
		cfg.Deploy.DefaultNodeVersion = def.Deploy.DefaultNodeVersion
	}
	if cfg.Deploy.SSHTimeout == 0 {
		cfg.Deploy.SSHTimeout = def.Deploy.SSHTimeout
	}
	if cfg.Deploy.CommandTimeout == 0 {
		cfg.Deploy.CommandTimeout = def.Deploy.CommandTimeout
	}
	cfg.CORS.AllowedOrigins = cleanList(cfg.CORS.AllowedOrigins)
	cfg.Web.Root = strings.TrimSpace(cfg.Web.Root)
}

func validateConfig(cfg *Config) error {
	if cfg.Listen == "" {
		return errors.New("listen is required")
	}
	if cfg.Database.Driver != "mysql" {
		return fmt.Errorf("unsupported database.driver %q", cfg.Database.Driver)
	}
	if cfg.Database.DSN == "" {
		return errors.New("database.dsn or GACCEL_PANEL_DATABASE_DSN is required")
	}
	if cfg.Security.MasterKey == "" {
		return errors.New("security.master_key or GACCEL_PANEL_MASTER_KEY is required")
	}
	if strings.HasPrefix(cfg.Security.MasterKey, "change-me") || strings.HasPrefix(cfg.Security.MasterKey, "replace-with") {
		return errors.New("security.master_key must be changed from the placeholder value")
	}
	if len(cfg.Security.BackendAPIKeys) == 0 {
		return errors.New("security.backend_api_keys or GACCEL_PANEL_BACKEND_API_KEYS is required")
	}
	for i, key := range cfg.Security.BackendAPIKeys {
		if key == "change-me-backend-api-key" || strings.HasPrefix(key, "replace-with") {
			return fmt.Errorf("security.backend_api_keys[%d] must be changed from the placeholder value", i)
		}
	}
	if cfg.Session.Secret == "" {
		return errors.New("session.secret or GACCEL_PANEL_SESSION_SECRET is required")
	}
	if strings.HasPrefix(cfg.Session.Secret, "change-me") || strings.HasPrefix(cfg.Session.Secret, "replace-with") {
		return errors.New("session.secret must be changed from the placeholder value")
	}
	if cfg.Session.TTL <= 0 {
		return errors.New("session.ttl must be > 0")
	}
	if cfg.NodeCommand.Secret == "" {
		return errors.New("node_command.secret or GACCEL_PANEL_COMMAND_SECRET is required")
	}
	if strings.HasPrefix(cfg.NodeCommand.Secret, "change-me") || strings.HasPrefix(cfg.NodeCommand.Secret, "replace-with") {
		return errors.New("node_command.secret must be changed from the placeholder value")
	}
	if cfg.NodeCommand.MaxClockSkew <= 0 {
		return errors.New("node_command.max_clock_skew must be > 0")
	}
	if cfg.NodeCommand.NonceTTL <= 0 {
		return errors.New("node_command.nonce_ttl must be > 0")
	}
	if cfg.NodeCommand.DefaultExpiresIn <= 0 {
		return errors.New("node_command.default_expires_in must be > 0")
	}
	if cfg.Deploy.SSHTimeout <= 0 {
		return errors.New("deploy.ssh_timeout must be > 0")
	}
	if cfg.Deploy.CommandTimeout <= 0 {
		return errors.New("deploy.command_timeout must be > 0")
	}
	return nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	return cleanList(strings.Split(value, ","))
}

func cleanList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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
