package quicserver

import (
	"errors"
	"fmt"
	"testing"

	"gaccel-node/internal/auth"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/protocol"
	"gaccel-node/internal/router"
)

func TestAuthErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "expired token",
			err:  fmt.Errorf("%w: %w", auth.ErrInvalidToken, auth.ErrTokenExpired),
			want: protocol.ErrorTokenExpired,
		},
		{
			name: "not active token",
			err:  fmt.Errorf("%w: %w", auth.ErrInvalidToken, auth.ErrTokenNotActive),
			want: protocol.ErrorTokenNotActive,
		},
		{
			name: "missing exp",
			err:  fmt.Errorf("%w: %w", auth.ErrInvalidToken, auth.ErrTokenMissingExpiration),
			want: protocol.ErrorTokenMissingExp,
		},
		{
			name: "connection limit",
			err:  metrics.ErrUserConnectionLimitExceeded,
			want: protocol.ErrorMaxConnectionsExceeded,
		},
		{
			name: "invalid token",
			err:  auth.ErrInvalidToken,
			want: protocol.ErrorTokenInvalid,
		},
		{
			name: "unknown auth error",
			err:  errors.New("backend unavailable"),
			want: protocol.ErrorAuthFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authErrorCode(tt.err); got != tt.want {
				t.Fatalf("authErrorCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFlowErrorCode(t *testing.T) {
	tests := []struct {
		name    string
		network string
		err     error
		want    string
	}{
		{
			name:    "permission denied",
			network: "udp",
			err:     auth.ErrPermissionDenied,
			want:    protocol.ErrorPermissionDenied,
		},
		{
			name:    "target denied",
			network: "tcp",
			err:     fmt.Errorf("%w: blocked", router.ErrTargetDenied),
			want:    protocol.ErrorTargetDenied,
		},
		{
			name:    "flow limit",
			network: "udp",
			err:     errors.New("max flows per connection exceeded"),
			want:    protocol.ErrorMaxFlowsExceeded,
		},
		{
			name:    "udp fallback",
			network: "udp",
			err:     errors.New("dial failed"),
			want:    protocol.ErrorOpenUDPFailed,
		},
		{
			name:    "tcp fallback",
			network: "tcp",
			err:     errors.New("dial failed"),
			want:    protocol.ErrorOpenTCPFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flowErrorCode(tt.network, tt.err); got != tt.want {
				t.Fatalf("flowErrorCode() = %q, want %q", got, tt.want)
			}
		})
	}
}
