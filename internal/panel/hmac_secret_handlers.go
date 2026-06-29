package panel

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var errInvalidNodeID = errors.New("invalid node id")

type NodeHMACSecretStatus struct {
	NodeID            string     `json:"node_id"`
	Configured        bool       `json:"configured"`
	Status            string     `json:"status"`
	Message           string     `json:"message"`
	Source            string     `json:"source,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	SecretFingerprint string     `json:"secret_fingerprint,omitempty"`
	CanClear          bool       `json:"can_clear"`
	CanSync           bool       `json:"can_sync"`
}

type NodeHMACSecretInput struct {
	HMACSecret string `json:"hmac_secret"`
}

func (s *Server) handleNodeHMACSecret(w http.ResponseWriter, r *http.Request, nodeID string, user *PanelUser) {
	node, err := s.getPanelNodeForSecret(r, nodeID)
	if err != nil {
		s.writeNodeSecretError(w, nodeID, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"hmac_secret": s.buildNodeHMACSecretStatus(*node)})
	case http.MethodPut:
		s.handleSyncNodeHMACSecret(w, r, *node, user)
	case http.MethodDelete:
		s.handleClearNodeHMACSecret(w, r, *node, user)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) getPanelNodeForSecret(r *http.Request, nodeID string) (*Node, error) {
	nodeID = strings.TrimSpace(nodeID)
	if !nodeIDPattern.MatchString(nodeID) {
		return nil, errInvalidNodeID
	}
	return s.store.GetNode(r.Context(), nodeID)
}

func (s *Server) writeNodeSecretError(w http.ResponseWriter, nodeID string, err error) {
	if err == nil {
		return
	}
	if err == errInvalidNodeID {
		writeError(w, http.StatusBadRequest, "invalid_node_id", "node_id is invalid")
		return
	}
	if err == ErrNotFound {
		writeError(w, http.StatusNotFound, "node_not_found", "node not found")
		return
	}
	s.logger.Error("get node hmac secret status", "node_id", nodeID, "error", err)
	writeError(w, http.StatusInternalServerError, "store_error", "get node failed")
}

func (s *Server) buildNodeHMACSecretStatus(node Node) NodeHMACSecretStatus {
	encrypted := strings.TrimSpace(node.HMACSecretEncrypted)
	status := NodeHMACSecretStatus{
		NodeID:     node.NodeID,
		Configured: encrypted != "",
		Status:     "missing",
		Message:    "节点 HMAC Secret 尚未同步，部署和客户端 token 签发不可用",
		Source:     strings.TrimSpace(node.HMACSecretSource),
		UpdatedAt:  node.HMACSecretUpdatedAt,
		CanClear:   false,
		CanSync:    true,
	}
	if encrypted == "" {
		return status
	}
	if s.secrets == nil {
		status.Status = "secret_box_unavailable"
		status.Message = "控制面板未配置 security.master_key，无法解密节点 HMAC Secret"
		status.CanClear = true
		return status
	}
	secret, err := s.secrets.Decrypt(encrypted)
	if err != nil {
		status.Status = classifySecretDecryptError(err)
		status.Message = "节点 HMAC Secret 加密副本无法解密，请重新同步密钥或清空损坏副本"
		status.CanClear = true
		return status
	}
	secret = strings.TrimSpace(secret)
	if len(secret) < 16 {
		status.Status = "invalid"
		status.Message = "节点 HMAC Secret 已保存但长度不合法，请重新同步"
		status.CanClear = true
		return status
	}
	status.Status = "ok"
	status.Message = "节点 HMAC Secret 已配置且可解密"
	status.SecretFingerprint = shortSecretFingerprint(secret)
	status.CanClear = true
	return status
}

func classifySecretDecryptError(err error) string {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "unsupported encrypted secret format") {
		return "unsupported_format"
	}
	if strings.Contains(message, "secret box is not configured") {
		return "secret_box_unavailable"
	}
	return "decrypt_failed"
}

func shortSecretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("%x", sum[:])[:16]
}

func (s *Server) handleSyncNodeHMACSecret(w http.ResponseWriter, r *http.Request, node Node, user *PanelUser) {
	var input NodeHMACSecretInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	secret := strings.TrimSpace(input.HMACSecret)
	if len(secret) < 16 {
		writeError(w, http.StatusBadRequest, "invalid_hmac_secret", "hmac_secret must be at least 16 characters")
		return
	}
	if s.secrets == nil {
		writeError(w, http.StatusServiceUnavailable, "secret_box_unavailable", "secret box is not configured")
		return
	}
	encrypted, err := s.secrets.Encrypt(secret)
	if err != nil {
		s.logger.Error("encrypt node hmac secret", "node_id", node.NodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "encrypt_secret_failed", "encrypt hmac_secret failed")
		return
	}
	now := time.Now().UTC()
	saved, err := s.store.SetNodeHMACSecret(r.Context(), node.NodeID, encrypted, "panel", now)
	if err != nil {
		if err == ErrNotFound {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("sync node hmac secret", "node_id", node.NodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "sync node hmac secret failed")
		return
	}
	s.recordNodeHMACSecretAudit(r, user, saved.NodeID, "panel.node.hmac_secret.sync", map[string]any{
		"hmac_secret": "[redacted]",
		"source":      "panel",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"node":        saved,
		"hmac_secret": s.buildNodeHMACSecretStatus(*saved),
	})
}

func (s *Server) handleClearNodeHMACSecret(w http.ResponseWriter, r *http.Request, node Node, user *PanelUser) {
	saved, err := s.store.ClearNodeHMACSecret(r.Context(), node.NodeID)
	if err != nil {
		if err == ErrNotFound {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("clear node hmac secret", "node_id", node.NodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "clear node hmac secret failed")
		return
	}
	s.recordNodeHMACSecretAudit(r, user, saved.NodeID, "panel.node.hmac_secret.clear", map[string]any{
		"cleared": true,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"node":        saved,
		"hmac_secret": s.buildNodeHMACSecretStatus(*saved),
	})
}

func (s *Server) recordNodeHMACSecretAudit(r *http.Request, user *PanelUser, nodeID string, action string, request any) {
	if s.store == nil {
		return
	}
	var operatorID *uint64
	if user != nil {
		operatorID = &user.ID
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		OperatorID: operatorID,
		Action:     action,
		TargetType: "node",
		TargetID:   nodeID,
		Request:    request,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", action, "node_id", nodeID, "error", err)
	}
}
