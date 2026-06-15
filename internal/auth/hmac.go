package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const hmacJWTAlg = "HS256"

type HMACAuthenticator struct {
	secret []byte
	leeway time.Duration
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type TokenClaims struct {
	Subject        string   `json:"sub,omitempty"`
	UserID         string   `json:"user_id,omitempty"`
	DeviceID       string   `json:"device_id,omitempty"`
	ExpiresAt      int64    `json:"exp,omitempty"`
	NotBefore      int64    `json:"nbf,omitempty"`
	IssuedAt       int64    `json:"iat,omitempty"`
	MaxConnections int      `json:"max_connections,omitempty"`
	RateLimitMbps  int      `json:"rate_limit_mbps,omitempty"`
	AllowTCP       *bool    `json:"allow_tcp,omitempty"`
	AllowUDP       *bool    `json:"allow_udp,omitempty"`
	Games          []string `json:"games,omitempty"`
	Regions        []string `json:"regions,omitempty"`
}

func NewHMACAuthenticator(secret string, leeway time.Duration) *HMACAuthenticator {
	return &HMACAuthenticator{
		secret: []byte(secret),
		leeway: leeway,
	}
}

func (a *HMACAuthenticator) Authenticate(token string) (*Principal, error) {
	claims, err := VerifyHMACToken(token, string(a.secret), a.leeway, time.Now())
	if err != nil {
		return nil, err
	}
	userID := claims.UserID
	if userID == "" {
		userID = claims.Subject
	}
	if userID == "" {
		return nil, fmt.Errorf("%w: missing user_id", ErrInvalidToken)
	}
	allowTCP := true
	if claims.AllowTCP != nil {
		allowTCP = *claims.AllowTCP
	}
	allowUDP := true
	if claims.AllowUDP != nil {
		allowUDP = *claims.AllowUDP
	}
	return &Principal{
		UserID:         userID,
		DeviceID:       claims.DeviceID,
		Token:          token,
		MaxConnections: claims.MaxConnections,
		RateLimitMbps:  claims.RateLimitMbps,
		AllowTCP:       allowTCP,
		AllowUDP:       allowUDP,
	}, nil
}

func SignHMACToken(claims TokenClaims, secret string) (string, error) {
	if secret == "" {
		return "", errors.New("hmac secret is required")
	}
	header := jwtHeader{Alg: hmacJWTAlg, Typ: "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	signature := signHMAC(unsigned, []byte(secret))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func VerifyHMACToken(token, secret string, leeway time.Duration, now time.Time) (*TokenClaims, error) {
	if secret == "" {
		return nil, fmt.Errorf("%w: missing hmac secret", ErrInvalidToken)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected three JWT parts", ErrInvalidToken)
	}
	unsigned := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature encoding", ErrInvalidToken)
	}
	expected := signHMAC(unsigned, []byte(secret))
	if !hmac.Equal(signature, expected) {
		return nil, fmt.Errorf("%w: signature mismatch", ErrInvalidToken)
	}

	headerData, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid header encoding", ErrInvalidToken)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerData, &header); err != nil {
		return nil, fmt.Errorf("%w: invalid header", ErrInvalidToken)
	}
	if header.Alg != hmacJWTAlg {
		return nil, fmt.Errorf("%w: unsupported alg %q", ErrInvalidToken, header.Alg)
	}
	if header.Typ != "" && header.Typ != "JWT" {
		return nil, fmt.Errorf("%w: unsupported typ %q", ErrInvalidToken, header.Typ)
	}

	claimsData, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid claims encoding", ErrInvalidToken)
	}
	var claims TokenClaims
	if err := json.Unmarshal(claimsData, &claims); err != nil {
		return nil, fmt.Errorf("%w: invalid claims", ErrInvalidToken)
	}

	if claims.ExpiresAt == 0 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, ErrTokenMissingExpiration)
	}
	if claims.MaxConnections < 0 {
		return nil, fmt.Errorf("%w: negative max_connections", ErrInvalidToken)
	}
	if claims.RateLimitMbps < 0 {
		return nil, fmt.Errorf("%w: negative rate_limit_mbps", ErrInvalidToken)
	}
	if leeway < 0 {
		leeway = 0
	}
	nowUnix := now.Unix()
	leewaySeconds := int64(leeway / time.Second)
	if nowUnix > claims.ExpiresAt+leewaySeconds {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, ErrTokenExpired)
	}
	if claims.NotBefore > 0 && nowUnix+leewaySeconds < claims.NotBefore {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, ErrTokenNotActive)
	}
	if claims.IssuedAt > 0 && nowUnix+leewaySeconds < claims.IssuedAt {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, ErrTokenIssuedInFuture)
	}
	return &claims, nil
}

func signHMAC(unsigned string, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return mac.Sum(nil)
}
