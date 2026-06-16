package tokenapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gaccel-node/internal/auth"
)

func TestIssueToken(t *testing.T) {
	cfg := testConfig()
	server := NewServer(cfg, slog.Default())
	now := time.Unix(1000, 0)

	token, expiresAt, ttl, err := server.issue(IssueRequest{
		UserID:         " user-1 ",
		DeviceID:       "device-1",
		TTLSeconds:     600,
		MaxConnections: 3,
		RateLimitMbps:  80,
	}, now)
	if err != nil {
		t.Fatalf("issue returned error: %v", err)
	}
	if ttl != 10*time.Minute {
		t.Fatalf("ttl = %s, want 10m", ttl)
	}
	if !expiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("expiresAt = %s, want %s", expiresAt, now.Add(10*time.Minute))
	}

	claims, err := auth.VerifyHMACToken(token, cfg.HMACSecret, 0, now)
	if err != nil {
		t.Fatalf("VerifyHMACToken returned error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("UserID = %q, want user-1", claims.UserID)
	}
	if claims.DeviceID != "device-1" {
		t.Fatalf("DeviceID = %q, want device-1", claims.DeviceID)
	}
	if claims.MaxConnections != 3 {
		t.Fatalf("MaxConnections = %d, want 3", claims.MaxConnections)
	}
	if claims.RateLimitMbps != 80 {
		t.Fatalf("RateLimitMbps = %d, want 80", claims.RateLimitMbps)
	}
}

func TestIssueTokenRejectsPolicyViolations(t *testing.T) {
	cfg := testConfig()
	server := NewServer(cfg, slog.Default())
	now := time.Unix(1000, 0)

	tests := []struct {
		name string
		req  IssueRequest
	}{
		{
			name: "missing user",
			req:  IssueRequest{},
		},
		{
			name: "ttl too large",
			req: IssueRequest{
				UserID:     "user-1",
				TTLSeconds: 7200,
			},
		},
		{
			name: "negative ttl",
			req: IssueRequest{
				UserID:     "user-1",
				TTLSeconds: -1,
			},
		},
		{
			name: "connections too high",
			req: IssueRequest{
				UserID:         "user-1",
				MaxConnections: 9,
			},
		},
		{
			name: "negative connections",
			req: IssueRequest{
				UserID:         "user-1",
				MaxConnections: -1,
			},
		},
		{
			name: "rate too high",
			req: IssueRequest{
				UserID:        "user-1",
				RateLimitMbps: 201,
			},
		},
		{
			name: "negative rate",
			req: IssueRequest{
				UserID:        "user-1",
				RateLimitMbps: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := server.issue(tt.req, now); err == nil {
				t.Fatal("issue returned nil error")
			}
		})
	}
}

func TestTokenEndpointRequiresBearerKey(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	body := bytes.NewBufferString(`{"user_id":"user-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/token", body)
	rec := httptest.NewRecorder()

	server.handleToken(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestTokenEndpointIssuesToken(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	body := bytes.NewBufferString(`{"user_id":"user-1","device_id":"device-1","ttl_seconds":300}`)
	req := httptest.NewRequest(http.MethodPost, "/token", body)
	req.Header.Set("Authorization", "Bearer api-key")
	rec := httptest.NewRecorder()

	server.handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp IssueResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("token is empty")
	}
	if resp.ExpiresInSeconds != 300 {
		t.Fatalf("ExpiresInSeconds = %d, want 300", resp.ExpiresInSeconds)
	}
}

func testConfig() *Config {
	cfg := DefaultConfig()
	cfg.HMACSecret = "secret"
	cfg.APIKeys = []string{"api-key"}
	return cfg
}
