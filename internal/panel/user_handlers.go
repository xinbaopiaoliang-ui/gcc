package panel

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type panelUserCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

type panelUserUpdateRequest struct {
	Role   string `json:"role"`
	Status string `json:"status"`
}

type panelUserPasswordResetRequest struct {
	NewPassword string `json:"new_password"`
}

func (s *Server) handlePanelUsers(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requirePanelAdmin(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		users, err := s.store.ListPanelUsers(r.Context())
		if err != nil {
			s.logger.Error("list panel users", "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "list users failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"users": users,
			"count": len(users),
		})
	case http.MethodPost:
		s.handleCreatePanelUser(w, r, admin)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handlePanelUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requirePanelAdmin(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	pathValue := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/panel/users/"), "/")
	parts := strings.Split(pathValue, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "panel user not found")
		return
	}
	userID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || userID == 0 {
		writeError(w, http.StatusBadRequest, "invalid_user_id", "panel user id is invalid")
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			s.handleUpdatePanelUser(w, r, admin, userID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "password" {
		switch r.Method {
		case http.MethodPost:
			s.handleResetPanelUserPassword(w, r, admin, userID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "panel user not found")
}

func (s *Server) handleCreatePanelUser(w http.ResponseWriter, r *http.Request, admin *PanelUser) {
	var req panelUserCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = PanelUserRoleOperator
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = PanelUserStatusActive
	}
	if err := validatePanelPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_hash_error", "hash password failed")
		return
	}
	user, err := s.store.CreatePanelUser(r.Context(), req.Username, string(hash), role, status)
	if err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "user_exists", "panel user already exists")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_user", err.Error())
		return
	}
	s.recordPanelUserAudit(r, admin, "panel.user.create", user.Username, map[string]any{
		"username": user.Username,
		"role":     user.Role,
		"status":   user.Status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleUpdatePanelUser(w http.ResponseWriter, r *http.Request, admin *PanelUser, userID uint64) {
	var req panelUserUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if userID == admin.ID {
		if req.Role != PanelUserRoleAdmin || req.Status != PanelUserStatusActive {
			writeError(w, http.StatusBadRequest, "cannot_modify_self", "admin cannot demote or disable itself")
			return
		}
	}
	user, err := s.store.UpdatePanelUser(r.Context(), userID, req.Role, req.Status)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "panel user not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_user", err.Error())
		return
	}
	s.recordPanelUserAudit(r, admin, "panel.user.update", user.Username, map[string]any{
		"user_id": user.ID,
		"role":    user.Role,
		"status":  user.Status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleResetPanelUserPassword(w http.ResponseWriter, r *http.Request, admin *PanelUser, userID uint64) {
	if userID == admin.ID {
		writeError(w, http.StatusBadRequest, "use_self_password_change", "use /api/panel/me/password to change your own password")
		return
	}
	var req panelUserPasswordResetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := validatePanelPassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_hash_error", "hash password failed")
		return
	}
	user, err := s.store.UpdatePanelUserPassword(r.Context(), userID, string(hash))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "panel user not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_user", err.Error())
		return
	}
	s.recordPanelUserAudit(r, admin, "panel.user.password_reset", user.Username, map[string]any{
		"user_id": user.ID,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"user":   user,
	})
}

func (s *Server) recordPanelUserAudit(r *http.Request, admin *PanelUser, action string, targetID string, request any) {
	if s.store == nil || admin == nil {
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		OperatorID: &admin.ID,
		Action:     action,
		TargetType: "panel_user",
		TargetID:   targetID,
		Request:    request,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", action, "target_id", targetID, "error", err, "at", time.Now().UTC())
	}
}
