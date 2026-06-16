package tokenapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gaccel-node/internal/auth"
)

type Server struct {
	cfg       *Config
	logger    *slog.Logger
	apiKeySet map[string]struct{}
}

type IssueRequest struct {
	UserID         string   `json:"user_id"`
	DeviceID       string   `json:"device_id,omitempty"`
	TTLSeconds     int64    `json:"ttl_seconds,omitempty"`
	MaxConnections int      `json:"max_connections,omitempty"`
	RateLimitMbps  int      `json:"rate_limit_mbps,omitempty"`
	AllowTCP       *bool    `json:"allow_tcp,omitempty"`
	AllowUDP       *bool    `json:"allow_udp,omitempty"`
	Games          []string `json:"games,omitempty"`
	Regions        []string `json:"regions,omitempty"`
}

type IssueResponse struct {
	Token            string    `json:"token"`
	TokenType        string    `json:"token_type"`
	UserID           string    `json:"user_id"`
	DeviceID         string    `json:"device_id,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
}

func NewServer(cfg *Config, logger *slog.Logger) *Server {
	keySet := make(map[string]struct{}, len(cfg.APIKeys))
	for _, key := range cfg.APIKeys {
		keySet[key] = struct{}{}
	}
	return &Server{
		cfg:       cfg,
		logger:    logger.With("component", "token-api"),
		apiKeySet: keySet,
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/token", s.handleToken)

	server := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("token api listening", "listen", s.cfg.Listen)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req IssueRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	token, expiresAt, ttl, err := s.issue(req, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, IssueResponse{
		Token:            token,
		TokenType:        "Bearer",
		UserID:           strings.TrimSpace(req.UserID),
		DeviceID:         strings.TrimSpace(req.DeviceID),
		ExpiresAt:        expiresAt,
		ExpiresInSeconds: int64(ttl / time.Second),
	})
}

func (s *Server) issue(req IssueRequest, now time.Time) (string, time.Time, time.Duration, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return "", time.Time{}, 0, errors.New("user_id is required")
	}
	ttl := s.cfg.Token.DefaultTTL
	if req.TTLSeconds < 0 {
		return "", time.Time{}, 0, errors.New("ttl_seconds must be >= 0")
	}
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if ttl <= 0 {
		return "", time.Time{}, 0, errors.New("ttl_seconds must be > 0")
	}
	if ttl > s.cfg.Token.MaxTTL {
		return "", time.Time{}, 0, fmt.Errorf("ttl_seconds must be <= %d", int64(s.cfg.Token.MaxTTL/time.Second))
	}

	maxConnections := s.cfg.Token.DefaultMaxConnections
	if req.MaxConnections < 0 {
		return "", time.Time{}, 0, errors.New("max_connections must be >= 0")
	}
	if req.MaxConnections > 0 {
		maxConnections = req.MaxConnections
	}
	if maxConnections < 0 || maxConnections > s.cfg.Token.MaxConnectionsLimit {
		return "", time.Time{}, 0, fmt.Errorf("max_connections must be between 0 and %d", s.cfg.Token.MaxConnectionsLimit)
	}

	rateLimit := s.cfg.Token.DefaultRateLimitMbps
	if req.RateLimitMbps < 0 {
		return "", time.Time{}, 0, errors.New("rate_limit_mbps must be >= 0")
	}
	if req.RateLimitMbps > 0 {
		rateLimit = req.RateLimitMbps
	}
	if rateLimit < 0 || rateLimit > s.cfg.Token.RateLimitMbpsLimit {
		return "", time.Time{}, 0, fmt.Errorf("rate_limit_mbps must be between 0 and %d", s.cfg.Token.RateLimitMbpsLimit)
	}

	allowTCP := s.cfg.Token.AllowTCP
	if req.AllowTCP != nil {
		allowTCP = *req.AllowTCP
	}
	if allowTCP && !s.cfg.Token.AllowTCP {
		return "", time.Time{}, 0, errors.New("allow_tcp is disabled by policy")
	}

	allowUDP := s.cfg.Token.AllowUDP
	if req.AllowUDP != nil {
		allowUDP = *req.AllowUDP
	}
	if allowUDP && !s.cfg.Token.AllowUDP {
		return "", time.Time{}, 0, errors.New("allow_udp is disabled by policy")
	}

	expiresAt := now.Add(ttl)
	token, err := auth.SignHMACToken(auth.TokenClaims{
		Subject:        userID,
		UserID:         userID,
		DeviceID:       strings.TrimSpace(req.DeviceID),
		IssuedAt:       now.Unix(),
		NotBefore:      now.Add(-5 * time.Second).Unix(),
		ExpiresAt:      expiresAt.Unix(),
		MaxConnections: maxConnections,
		RateLimitMbps:  rateLimit,
		AllowTCP:       &allowTCP,
		AllowUDP:       &allowUDP,
		Games:          cleanList(req.Games),
		Regions:        cleanList(req.Regions),
	}, s.cfg.HMACSecret)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	return token, expiresAt, ttl, nil
}

func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	key := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	for allowed := range s.apiKeySet {
		if subtle.ConstantTimeCompare([]byte(key), []byte(allowed)) == 1 {
			return true
		}
	}
	return false
}

func cleanList(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
