package tokenapi

import "testing"

func TestValidateConfigRejectsPlaceholders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HMACSecret = "replace-with-secret"
	cfg.APIKeys = []string{"api-key"}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("validateConfig returned nil error for placeholder hmac secret")
	}

	cfg = DefaultConfig()
	cfg.HMACSecret = "secret"
	cfg.APIKeys = []string{"change-me-api-key"}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("validateConfig returned nil error for placeholder api key")
	}
}

func TestApplyEnvOverridesConfigSecrets(t *testing.T) {
	t.Setenv("GACCEL_HMAC_SECRET", "env-secret")
	t.Setenv("GACCEL_TOKEN_API_KEYS", "env-key-1, env-key-2")

	cfg := DefaultConfig()
	cfg.HMACSecret = "file-secret"
	cfg.APIKeys = []string{"file-key"}
	applyEnv(cfg)

	if cfg.HMACSecret != "env-secret" {
		t.Fatalf("HMACSecret = %q, want env-secret", cfg.HMACSecret)
	}
	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "env-key-1" || cfg.APIKeys[1] != "env-key-2" {
		t.Fatalf("APIKeys = %#v, want env keys", cfg.APIKeys)
	}
}
