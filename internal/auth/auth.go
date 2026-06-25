package auth

import (
	"errors"

	"gaccel-node/internal/config"
)

var ErrInvalidToken = errors.New("invalid token")
var ErrPermissionDenied = errors.New("permission denied")
var ErrTokenExpired = errors.New("token expired")
var ErrTokenNotActive = errors.New("token not active")
var ErrTokenMissingExpiration = errors.New("token missing expiration")
var ErrTokenIssuedInFuture = errors.New("token issued in the future")

type Principal struct {
	UserID         string
	DeviceID       string
	Token          string
	MaxConnections int
	RateLimitMbps  int
	AllowTCP       bool
	AllowUDP       bool
	GameIDs        []string
	PolicyIDs      []string
	ConfigRevision string
}

type Authenticator interface {
	Authenticate(token string) (*Principal, error)
}

func New(cfg config.AuthConfig) Authenticator {
	switch cfg.Mode {
	case "dev":
		return NewDevAuthenticator(cfg.DevTokens)
	case "hmac":
		return NewHMACAuthenticator(cfg.HMACSecret, cfg.TokenLeeway)
	default:
		return NewDevAuthenticator(cfg.DevTokens)
	}
}
