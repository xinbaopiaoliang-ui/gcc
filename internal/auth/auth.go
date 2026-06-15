package auth

import (
	"errors"

	"gaccel-node/internal/config"
)

var ErrInvalidToken = errors.New("invalid token")

type Principal struct {
	UserID string
	Token  string
}

type Authenticator interface {
	Authenticate(token string) (*Principal, error)
}

func New(cfg config.AuthConfig) Authenticator {
	switch cfg.Mode {
	case "dev":
		return NewDevAuthenticator(cfg.DevTokens)
	default:
		return NewDevAuthenticator(cfg.DevTokens)
	}
}
