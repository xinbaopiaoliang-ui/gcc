package panel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	PanelUserStatusActive   = "active"
	PanelUserStatusDisabled = "disabled"

	PanelUserRoleAdmin    = "admin"
	PanelUserRoleOperator = "operator"
	PanelUserRoleViewer   = "viewer"
)

type PanelUser struct {
	ID           uint64    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type panelSessionClaims struct {
	Subject  string `json:"sub,omitempty"`
	UserID   uint64 `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
	IssuedAt int64  `json:"iat,omitempty"`
}

type panelJWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func validatePanelUsername(username string) error {
	if len(username) < 3 || len(username) > 64 {
		return fmt.Errorf("username length must be 3-64")
	}
	for _, ch := range username {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		switch ch {
		case '_', '-', '.', '@':
			continue
		default:
			return fmt.Errorf("username contains unsupported character")
		}
	}
	return nil
}

func normalizePanelRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = PanelUserRoleAdmin
	}
	switch role {
	case PanelUserRoleAdmin, PanelUserRoleOperator, PanelUserRoleViewer:
		return role, nil
	default:
		return "", fmt.Errorf("panel user role %q is not allowed", role)
	}
}

func normalizePanelUserStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		status = PanelUserStatusActive
	}
	switch status {
	case PanelUserStatusActive, PanelUserStatusDisabled:
		return status, nil
	default:
		return "", fmt.Errorf("panel user status %q is not allowed", status)
	}
}

func validatePanelPassword(password string) error {
	if len(password) < 10 || len(password) > 128 {
		return fmt.Errorf("password length must be 10-128")
	}
	if password != strings.TrimSpace(password) {
		return fmt.Errorf("password cannot start or end with whitespace")
	}
	return nil
}

func verifyPanelPassword(user PanelUser, password string) bool {
	if strings.TrimSpace(password) == "" || strings.TrimSpace(user.PasswordHash) == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
}

func (s *Server) handlePanelLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	username := strings.TrimSpace(req.Username)
	now := time.Now().UTC()
	attemptKey := loginAttemptKey(username, clientIP(r))
	if wait, blocked := s.loginLimiter.blocked(attemptKey, now); blocked {
		writeError(w, http.StatusTooManyRequests, "login_rate_limited", fmt.Sprintf("too many login attempts; retry after %s", wait.Round(time.Second)))
		return
	}
	user, err := s.store.GetPanelUserByUsername(r.Context(), username)
	if err != nil || user.Status != PanelUserStatusActive || !verifyPanelPassword(*user, req.Password) {
		s.loginLimiter.recordFailure(attemptKey, now)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "username or password is invalid")
		return
	}
	s.loginLimiter.recordSuccess(attemptKey)
	token, expires, err := s.newPanelAccessToken(*user, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_error", "create session failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		OperatorID: &user.ID,
		Action:     "panel.login",
		TargetType: "panel_user",
		TargetID:   user.Username,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.login", "username", user.Username, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"token":              token,
		"access_token":       token,
		"token_type":         "Bearer",
		"expires_at":         expires,
		"expires_in_seconds": int(time.Until(expires).Seconds()),
		"user":               user,
	})
}

func (s *Server) handlePanelLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if user, ok := s.currentPanelUser(r); ok && s.store != nil {
		if err := s.store.RecordAudit(r.Context(), AuditLog{
			OperatorID: &user.ID,
			Action:     "panel.logout",
			TargetType: "panel_user",
			TargetID:   user.Username,
			IP:         clientIP(r),
			UserAgent:  r.UserAgent(),
		}); err != nil {
			s.logger.Warn("record audit", "action", "panel.logout", "username", user.Username, "error", err)
		}
	}
	http.SetCookie(w, s.clearPanelSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handlePanelMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handlePanelPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if !verifyPanelPassword(*user, req.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "invalid_current_password", "current password is invalid")
		return
	}
	if err := validatePanelPassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if verifyPanelPassword(*user, req.NewPassword) {
		writeError(w, http.StatusBadRequest, "password_reused", "new password must be different from current password")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_hash_error", "hash password failed")
		return
	}
	updated, err := s.store.UpdatePanelUserPassword(r.Context(), user.ID, string(hash))
	if err != nil {
		s.logger.Error("update panel password", "user_id", user.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "update password failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		OperatorID: &user.ID,
		Action:     "panel.password.change",
		TargetType: "panel_user",
		TargetID:   user.Username,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.password.change", "username", user.Username, "error", err)
	}
	now := time.Now().UTC()
	token, expires, err := s.newPanelAccessToken(*updated, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_error", "refresh session failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"token":              token,
		"access_token":       token,
		"token_type":         "Bearer",
		"expires_at":         expires,
		"expires_in_seconds": int(time.Until(expires).Seconds()),
		"user":               updated,
	})
}

func (s *Server) requirePanelUser(w http.ResponseWriter, r *http.Request) (*PanelUser, bool) {
	user, ok := s.currentPanelUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "login_required", "login required")
		return nil, false
	}
	return user, true
}

func (s *Server) currentPanelUser(r *http.Request) (*PanelUser, bool) {
	if s.store == nil {
		return nil, false
	}
	claims, err := s.parsePanelBearerToken(r)
	if err != nil {
		claims, err = s.parsePanelSessionCookie(r)
		if err != nil {
			return nil, false
		}
	}
	user, err := s.store.GetPanelUserByID(r.Context(), claims.UserID)
	if err != nil || user.Status != PanelUserStatusActive {
		return nil, false
	}
	return user, true
}

func (s *Server) newPanelAccessToken(user PanelUser, now time.Time) (string, time.Time, error) {
	ttl := s.cfg.Session.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	expires := now.Add(ttl)
	header, err := json.Marshal(panelJWTHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", time.Time{}, err
	}
	claims := panelSessionClaims{
		Subject:  fmt.Sprintf("%d", user.ID),
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Exp:      expires.Unix(),
		IssuedAt: now.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	headerValue := base64.RawURLEncoding.EncodeToString(header)
	payloadValue := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := headerValue + "." + payloadValue
	return signingInput + "." + s.signPanelSession(signingInput), expires, nil
}

func (s *Server) newPanelSessionCookie(user PanelUser, now time.Time, r *http.Request) (*http.Cookie, error) {
	ttl := s.cfg.Session.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	expires := now.Add(ttl)
	claims := panelSessionClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Exp:      expires.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}
	payloadValue := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.signPanelSession(payloadValue)
	return &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    payloadValue + "." + signature,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	}, nil
}

func (s *Server) clearPanelSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	}
}

func (s *Server) parsePanelBearerToken(r *http.Request) (panelSessionClaims, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return panelSessionClaims{}, errors.New("missing bearer token")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return panelSessionClaims{}, errors.New("invalid jwt format")
	}
	signingInput := parts[0] + "." + parts[1]
	expected := s.signPanelSession(signingInput)
	if !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return panelSessionClaims{}, errors.New("invalid jwt signature")
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return panelSessionClaims{}, err
	}
	var jwtHeader panelJWTHeader
	if err := json.Unmarshal(rawHeader, &jwtHeader); err != nil {
		return panelSessionClaims{}, err
	}
	if jwtHeader.Alg != "HS256" || jwtHeader.Typ != "JWT" {
		return panelSessionClaims{}, errors.New("unsupported jwt header")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return panelSessionClaims{}, err
	}
	var claims panelSessionClaims
	if err := json.Unmarshal(rawPayload, &claims); err != nil {
		return panelSessionClaims{}, err
	}
	if claims.UserID == 0 || claims.Exp <= time.Now().UTC().Unix() {
		return panelSessionClaims{}, fmt.Errorf("jwt expired")
	}
	return claims, nil
}

func (s *Server) parsePanelSessionCookie(r *http.Request) (panelSessionClaims, error) {
	cookie, err := r.Cookie(s.cfg.Session.CookieName)
	if err != nil {
		return panelSessionClaims{}, err
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return panelSessionClaims{}, errors.New("invalid session format")
	}
	expected := s.signPanelSession(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return panelSessionClaims{}, errors.New("invalid session signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return panelSessionClaims{}, err
	}
	var claims panelSessionClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return panelSessionClaims{}, err
	}
	if claims.UserID == 0 || claims.Exp <= time.Now().UTC().Unix() {
		return panelSessionClaims{}, fmt.Errorf("session expired")
	}
	return claims, nil
}

func (s *Server) signPanelSession(payload string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.Session.Secret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
