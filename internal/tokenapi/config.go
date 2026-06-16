package tokenapi

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen     string      `yaml:"listen"`
	HMACSecret string      `yaml:"hmac_secret"`
	APIKeys    []string    `yaml:"api_keys"`
	Token      TokenConfig `yaml:"token"`
}

type TokenConfig struct {
	DefaultTTL            time.Duration `yaml:"default_ttl"`
	MaxTTL                time.Duration `yaml:"max_ttl"`
	DefaultMaxConnections int           `yaml:"default_max_connections"`
	MaxConnectionsLimit   int           `yaml:"max_connections_limit"`
	DefaultRateLimitMbps  int           `yaml:"default_rate_limit_mbps"`
	RateLimitMbpsLimit    int           `yaml:"rate_limit_mbps_limit"`
	AllowTCP              bool          `yaml:"allow_tcp"`
	AllowUDP              bool          `yaml:"allow_udp"`
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
		Listen: "127.0.0.1:8088",
		Token: TokenConfig{
			DefaultTTL:            15 * time.Minute,
			MaxTTL:                time.Hour,
			DefaultMaxConnections: 2,
			MaxConnectionsLimit:   8,
			DefaultRateLimitMbps:  50,
			RateLimitMbpsLimit:    200,
			AllowTCP:              true,
			AllowUDP:              true,
		},
	}
}

func applyEnv(cfg *Config) {
	if secret := os.Getenv("GACCEL_HMAC_SECRET"); secret != "" {
		cfg.HMACSecret = secret
	}
	if keys := splitCSV(os.Getenv("GACCEL_TOKEN_API_KEYS")); len(keys) > 0 {
		cfg.APIKeys = keys
	}
}

func applyDefaults(cfg *Config) {
	def := DefaultConfig()
	if cfg.Listen == "" {
		cfg.Listen = def.Listen
	}
	if cfg.Token.DefaultTTL == 0 {
		cfg.Token.DefaultTTL = def.Token.DefaultTTL
	}
	if cfg.Token.MaxTTL == 0 {
		cfg.Token.MaxTTL = def.Token.MaxTTL
	}
	if cfg.Token.DefaultMaxConnections == 0 {
		cfg.Token.DefaultMaxConnections = def.Token.DefaultMaxConnections
	}
	if cfg.Token.MaxConnectionsLimit == 0 {
		cfg.Token.MaxConnectionsLimit = def.Token.MaxConnectionsLimit
	}
	if cfg.Token.DefaultRateLimitMbps == 0 {
		cfg.Token.DefaultRateLimitMbps = def.Token.DefaultRateLimitMbps
	}
	if cfg.Token.RateLimitMbpsLimit == 0 {
		cfg.Token.RateLimitMbpsLimit = def.Token.RateLimitMbpsLimit
	}
}

func validateConfig(cfg *Config) error {
	if cfg.Listen == "" {
		return errors.New("listen is required")
	}
	if cfg.HMACSecret == "" {
		return errors.New("hmac_secret or GACCEL_HMAC_SECRET is required")
	}
	if strings.HasPrefix(cfg.HMACSecret, "replace-with") {
		return errors.New("hmac_secret must be changed from the placeholder value")
	}
	if len(cfg.APIKeys) == 0 {
		return errors.New("api_keys or GACCEL_TOKEN_API_KEYS is required")
	}
	if cfg.Token.DefaultTTL <= 0 {
		return errors.New("token.default_ttl must be > 0")
	}
	if cfg.Token.MaxTTL < cfg.Token.DefaultTTL {
		return errors.New("token.max_ttl must be >= token.default_ttl")
	}
	if cfg.Token.DefaultMaxConnections < 0 || cfg.Token.MaxConnectionsLimit < cfg.Token.DefaultMaxConnections {
		return errors.New("token max connections defaults and limits are invalid")
	}
	if cfg.Token.DefaultRateLimitMbps < 0 || cfg.Token.RateLimitMbpsLimit < cfg.Token.DefaultRateLimitMbps {
		return errors.New("token rate limit defaults and limits are invalid")
	}
	for i, key := range cfg.APIKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("api_keys[%d] must not be empty", i)
		}
		if key == "change-me-api-key" || strings.HasPrefix(key, "replace-with") {
			return fmt.Errorf("api_keys[%d] must be changed from the placeholder value", i)
		}
	}
	return nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
