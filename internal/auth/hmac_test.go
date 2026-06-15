package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestHMACTokenRoundTrip(t *testing.T) {
	allowTCP := true
	allowUDP := false
	now := time.Unix(1000, 0)
	token, err := SignHMACToken(TokenClaims{
		UserID:         "user-1",
		DeviceID:       "device-1",
		IssuedAt:       now.Unix(),
		NotBefore:      now.Add(-time.Second).Unix(),
		ExpiresAt:      now.Add(time.Minute).Unix(),
		MaxConnections: 2,
		RateLimitMbps:  50,
		AllowTCP:       &allowTCP,
		AllowUDP:       &allowUDP,
	}, "secret")
	if err != nil {
		t.Fatalf("SignHMACToken returned error: %v", err)
	}

	claims, err := VerifyHMACToken(token, "secret", 30*time.Second, now)
	if err != nil {
		t.Fatalf("VerifyHMACToken returned error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("UserID = %q, want user-1", claims.UserID)
	}
	if claims.DeviceID != "device-1" {
		t.Fatalf("DeviceID = %q, want device-1", claims.DeviceID)
	}
	if claims.MaxConnections != 2 {
		t.Fatalf("MaxConnections = %d, want 2", claims.MaxConnections)
	}
	if claims.RateLimitMbps != 50 {
		t.Fatalf("RateLimitMbps = %d, want 50", claims.RateLimitMbps)
	}
	if claims.AllowUDP == nil || *claims.AllowUDP {
		t.Fatal("AllowUDP should be explicitly false")
	}
}

func TestHMACTokenRejectsExpiredToken(t *testing.T) {
	now := time.Unix(1000, 0)
	token, err := SignHMACToken(TokenClaims{
		UserID:    "user-1",
		ExpiresAt: now.Add(-time.Minute).Unix(),
	}, "secret")
	if err != nil {
		t.Fatalf("SignHMACToken returned error: %v", err)
	}

	if _, err := VerifyHMACToken(token, "secret", 0, now); err == nil {
		t.Fatal("VerifyHMACToken returned nil error for expired token")
	} else if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("VerifyHMACToken error = %v, want ErrTokenExpired", err)
	}
}

func TestHMACTokenRequiresExpiration(t *testing.T) {
	now := time.Unix(1000, 0)
	token, err := SignHMACToken(TokenClaims{
		UserID: "user-1",
	}, "secret")
	if err != nil {
		t.Fatalf("SignHMACToken returned error: %v", err)
	}

	if _, err := VerifyHMACToken(token, "secret", 0, now); err == nil {
		t.Fatal("VerifyHMACToken returned nil error for token without exp")
	} else if !errors.Is(err, ErrTokenMissingExpiration) {
		t.Fatalf("VerifyHMACToken error = %v, want ErrTokenMissingExpiration", err)
	}
}

func TestHMACTokenRejectsNotActiveToken(t *testing.T) {
	now := time.Unix(1000, 0)
	token, err := SignHMACToken(TokenClaims{
		UserID:    "user-1",
		NotBefore: now.Add(time.Minute).Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
	}, "secret")
	if err != nil {
		t.Fatalf("SignHMACToken returned error: %v", err)
	}

	if _, err := VerifyHMACToken(token, "secret", 0, now); err == nil {
		t.Fatal("VerifyHMACToken returned nil error for not active token")
	} else if !errors.Is(err, ErrTokenNotActive) {
		t.Fatalf("VerifyHMACToken error = %v, want ErrTokenNotActive", err)
	}
}

func TestHMACTokenHonorsLeeway(t *testing.T) {
	now := time.Unix(1000, 0)
	token, err := SignHMACToken(TokenClaims{
		UserID:    "user-1",
		ExpiresAt: now.Add(-10 * time.Second).Unix(),
	}, "secret")
	if err != nil {
		t.Fatalf("SignHMACToken returned error: %v", err)
	}

	if _, err := VerifyHMACToken(token, "secret", 30*time.Second, now); err != nil {
		t.Fatalf("VerifyHMACToken returned error inside leeway: %v", err)
	}
}

func TestHMACTokenAcceptsMissingTypeHeader(t *testing.T) {
	now := time.Unix(1000, 0)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"user_id":"user-1","exp":%d}`, now.Add(time.Minute).Unix())))
	unsigned := header + "." + claims
	token := unsigned + "." + base64.RawURLEncoding.EncodeToString(signHMAC(unsigned, []byte("secret")))

	if _, err := VerifyHMACToken(token, "secret", 0, now); err != nil {
		t.Fatalf("VerifyHMACToken returned error for missing typ header: %v", err)
	}
}

func TestHMACTokenRejectsTamperedSignature(t *testing.T) {
	now := time.Unix(1000, 0)
	token, err := SignHMACToken(TokenClaims{
		UserID:    "user-1",
		ExpiresAt: now.Add(time.Minute).Unix(),
	}, "secret")
	if err != nil {
		t.Fatalf("SignHMACToken returned error: %v", err)
	}
	parts := strings.Split(token, ".")
	parts[2] = "tampered"

	if _, err := VerifyHMACToken(strings.Join(parts, "."), "secret", 0, now); err == nil {
		t.Fatal("VerifyHMACToken returned nil error for tampered token")
	}
}
