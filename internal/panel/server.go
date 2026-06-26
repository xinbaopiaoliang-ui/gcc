package panel

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gaccel-node/internal/panelcommand"
)

type Server struct {
	cfg          *Config
	logger       *slog.Logger
	version      string
	store        NodeStore
	secrets      *SecretBox
	loginLimiter *loginAttemptLimiter
}

func NewServer(cfg *Config, logger *slog.Logger, version string, stores ...NodeStore) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var store NodeStore
	if len(stores) > 0 {
		store = stores[0]
	}
	var secrets *SecretBox
	if strings.TrimSpace(cfg.Security.MasterKey) != "" {
		box, err := NewSecretBox(cfg.Security.MasterKey)
		if err == nil {
			secrets = box
		}
	}
	return &Server{
		cfg:          cfg,
		logger:       logger.With("component", "panel"),
		version:      version,
		store:        store,
		secrets:      secrets,
		loginLimiter: newLoginAttemptLimiter(5, 10*time.Minute, 5*time.Minute),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	server := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("panel listening", "listen", s.cfg.Listen)
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

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.applyCORSHeaders(w, r) && r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) applyCORSHeaders(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || !s.corsOriginAllowed(origin) {
		return false
	}
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	header.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
	header.Set("Access-Control-Max-Age", "600")
	header.Add("Vary", "Origin")
	return true
}

func (s *Server) corsOriginAllowed(origin string) bool {
	for _, allowed := range s.cfg.CORS.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/panel/health", s.handleHealth)
	mux.HandleFunc("/api/panel/login", s.handlePanelLogin)
	mux.HandleFunc("/api/panel/logout", s.handlePanelLogout)
	mux.HandleFunc("/api/panel/me", s.handlePanelMe)
	mux.HandleFunc("/api/panel/me/password", s.handlePanelPassword)
	mux.HandleFunc("/api/panel/users", s.handlePanelUsers)
	mux.HandleFunc("/api/panel/users/", s.handlePanelUser)
	mux.HandleFunc("/api/panel/security/overview", s.handlePanelSecurityOverview)
	mux.HandleFunc("/api/panel/security/backend-api-keys", s.handlePanelBackendAPIKeys)
	mux.HandleFunc("/api/panel/system/check", s.handlePanelSystemCheck)
	mux.HandleFunc("/api/panel/token-defaults", s.handlePanelTokenDefaults)
	mux.HandleFunc("/api/panel/client-sessions", s.handlePanelClientSessions)
	mux.HandleFunc("/api/panel/traffic/overview", s.handlePanelTrafficOverview)
	mux.HandleFunc("/api/panel/policy-revisions/validate", s.handlePanelPolicyValidation)
	mux.HandleFunc("/api/panel/policy-revisions", s.handlePanelPolicyRevisions)
	mux.HandleFunc("/api/panel/nodes", s.handlePanelNodes)
	mux.HandleFunc("/api/panel/nodes/", s.handlePanelNode)
	mux.HandleFunc("/api/panel/tasks/", s.handlePanelTask)
	mux.HandleFunc("/api/backend/system/check", s.handleBackendSystemCheck)
	mux.HandleFunc("/api/backend/token-defaults", s.handleBackendTokenDefaults)
	mux.HandleFunc("/api/backend/client-sessions", s.handleBackendClientSessions)
	mux.HandleFunc("/api/backend/policy-revisions/validate", s.handleBackendPolicyValidation)
	mux.HandleFunc("/api/backend/policy-revisions", s.handleBackendPolicyRevisions)
	mux.HandleFunc("/api/backend/nodes", s.handleBackendNodes)
	mux.HandleFunc("/api/backend/nodes/", s.handleBackendNode)
	mux.HandleFunc("/api/nodes/report", s.handleNodeReport)
	mux.HandleFunc("/api/nodes/commands", s.handleNodeCommands)
	if strings.TrimSpace(s.cfg.Web.Root) != "" {
		mux.HandleFunc("/", s.handleWeb)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "gaccel-panel",
		"version": s.version,
	})
}

func (s *Server) handlePanelNodes(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleListNodes(w, r)
	case http.MethodPost:
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		s.handleUpsertNode(w, r, "", "panel.node.upsert")
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handlePanelNode(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	nodeID := strings.TrimPrefix(r.URL.Path, "/api/panel/nodes/")
	if nodeID == "" || strings.Contains(nodeID, "/") {
		s.handlePanelNodeSubresource(w, r, nodeID, user)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetNode(w, r, nodeID)
	case http.MethodPut:
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		s.handleUpsertNode(w, r, nodeID, "panel.node.upsert")
	case http.MethodDelete:
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		s.handleDeleteNode(w, r, nodeID, "panel.node.delete")
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handlePanelNodeSubresource(w http.ResponseWriter, r *http.Request, pathValue string, user *PanelUser) {
	parts := strings.Split(strings.Trim(pathValue, "/"), "/")
	if len(parts) == 2 {
		switch parts[1] {
		case "reports":
			s.handleListNodeReports(w, r, parts[0])
			return
		case "tasks":
			s.handleListNodeTasks(w, r, parts[0])
			return
		case "sync-status":
			s.handleGetNodeSyncStatus(w, r, parts[0])
			return
		case "diagnostics":
			s.handleGetNodeDiagnostics(w, r, parts[0])
			return
		case "network-diagnostics":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleGetNodeNetworkDiagnostics(w, r, parts[0])
			return
		case "connectivity-probe":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleGetNodeConnectivityProbe(w, r, parts[0])
			return
		case "repair-admin":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleCreateRepairAdminTask(w, r, parts[0])
			return
		case "tune-udp-buffer":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleCreateTuneUDPBufferTask(w, r, parts[0])
			return
		case "credential":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleNodeCredential(w, r, parts[0])
			return
		case "deploy":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleCreateDeployTask(w, r, parts[0])
			return
		case "update":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleCreateUpdateTask(w, r, parts[0])
			return
		case "desired-policy":
			if !panelUserHasRole(user, PanelUserRoleAdmin) {
				writeError(w, http.StatusForbidden, "forbidden", "permission denied")
				return
			}
			s.handleSetNodeDesiredPolicy(w, r, parts[0], "panel.node.desired_policy")
			return
		}
	}
	if len(parts) == 3 && parts[1] == "credential" && parts[2] == "test" {
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		s.handleTestNodeCredential(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "commands" && parts[2] == "apply_policy" {
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		s.handleCreateApplyPolicyTask(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "node not found")
}

func (s *Server) handlePanelTask(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/panel/tasks/"), "/"), "/")
	if len(parts) == 2 && parts[1] == "logs" {
		s.handleListTaskLogs(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "retry" {
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		s.handleRetryTask(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "task not found")
}

func (s *Server) handleBackendNodes(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleUpsertNode(w, r, "", "backend.node.upsert")
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleBackendNode(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	nodeID := strings.TrimPrefix(r.URL.Path, "/api/backend/nodes/")
	parts := strings.Split(strings.Trim(nodeID, "/"), "/")
	if len(parts) == 2 && parts[1] == "desired-policy" {
		s.handleSetNodeDesiredPolicy(w, r, parts[0], "backend.node.desired_policy")
		return
	}
	if len(parts) == 2 && parts[1] == "sync-status" {
		s.handleGetNodeSyncStatus(w, r, parts[0])
		return
	}
	if nodeID == "" || strings.Contains(nodeID, "/") {
		writeError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpsertNode(w, r, nodeID, "backend.node.upsert")
	case http.MethodDelete:
		s.handleDeleteNode(w, r, nodeID, "backend.node.delete")
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handlePanelPolicyRevisions(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet && !panelUserHasRole(user, PanelUserRoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "permission denied")
		return
	}
	s.handlePolicyRevisions(w, r, PolicySourceManual, "panel.policy_revision.upsert")
}

func (s *Server) handleBackendPolicyRevisions(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	s.handlePolicyRevisions(w, r, PolicySourceBackend, "backend.policy_revision.upsert")
}

func (s *Server) handlePolicyRevisions(w http.ResponseWriter, r *http.Request, source string, action string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 50, 200)
		policies, err := s.store.ListPolicyRevisions(r.Context(), limit)
		if err != nil {
			s.logger.Error("list policy revisions", "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "list policy revisions failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"policy_revisions": policies,
			"count":            len(policies),
		})
	case http.MethodPost:
		var input PolicyRevisionInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		input.Source = source
		policy, err := s.store.UpsertPolicyRevision(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_policy_revision", err.Error())
			return
		}
		if err := s.store.RecordAudit(r.Context(), AuditLog{
			Action:     action,
			TargetType: "policy_revision",
			TargetID:   policy.Revision,
			Request:    input,
			IP:         clientIP(r),
			UserAgent:  r.UserAgent(),
		}); err != nil {
			s.logger.Warn("record audit", "action", action, "revision", policy.Revision, "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy_revision": policy})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

type DesiredPolicyRequest struct {
	Revision   string `json:"revision"`
	CreateTask *bool  `json:"create_task,omitempty"`
	Priority   int    `json:"priority,omitempty"`
}

func (s *Server) handleSetNodeDesiredPolicy(w http.ResponseWriter, r *http.Request, nodeID string, action string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if !nodeIDPattern.MatchString(nodeID) {
		writeError(w, http.StatusBadRequest, "invalid_node_id", "node_id is invalid")
		return
	}
	var req DesiredPolicyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	revision := strings.TrimSpace(req.Revision)
	policy, err := s.store.GetPolicyRevision(r.Context(), revision)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "policy_revision_not_found", "policy revision not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_policy_revision", err.Error())
		return
	}
	now := time.Now().UTC()
	nodePolicy, err := s.store.SetNodeDesiredPolicy(r.Context(), nodeID, policy.Revision, now)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("set node desired policy", "node_id", nodeID, "revision", policy.Revision, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "set desired policy failed")
		return
	}
	createTask := true
	if req.CreateTask != nil {
		createTask = *req.CreateTask
	}
	var task *NodeTask
	if createTask {
		taskInput, err := NewApplyPolicyTask(nodeID, ApplyPolicyTaskRequest{
			Revision:          policy.Revision,
			RoutePoliciesYAML: policy.RoutePoliciesYAML,
			SHA256:            policy.SHA256,
			Priority:          req.Priority,
		}, now)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
			return
		}
		task, err = s.store.CreateNodeTask(r.Context(), taskInput)
		if err != nil {
			s.logger.Error("create desired policy task", "node_id", nodeID, "revision", policy.Revision, "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "create policy task failed")
			return
		}
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     action,
		TargetType: "node",
		TargetID:   nodeID,
		Request: map[string]any{
			"revision":    policy.Revision,
			"create_task": createTask,
			"priority":    req.Priority,
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", action, "node_id", nodeID, "revision", policy.Revision, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"policy_revision": policy,
		"node_policy":     nodePolicy,
		"task":            task,
	})
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	filter := NodeListFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Region: strings.TrimSpace(r.URL.Query().Get("region")),
		Limit:  100,
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		filter.Limit = limit
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err := strconv.Atoi(value)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "invalid_offset", "offset must be a non-negative integer")
			return
		}
		filter.Offset = offset
	}
	nodes, err := s.store.ListNodes(r.Context(), filter)
	if err != nil {
		s.logger.Error("list nodes", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list nodes failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "node not found")
			return
		}
		s.logger.Error("get node", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get node failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": node})
}

func (s *Server) handleUpsertNode(w http.ResponseWriter, r *http.Request, pathNodeID string, action string) {
	var input NodeInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if pathNodeID != "" {
		if strings.TrimSpace(input.NodeID) != "" && strings.TrimSpace(input.NodeID) != pathNodeID {
			writeError(w, http.StatusBadRequest, "node_id_mismatch", "body node_id does not match path node_id")
			return
		}
		input.NodeID = pathNodeID
	}
	node, err := NewNodeFromInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node", err.Error())
		return
	}
	if err := s.applyNodeHMACSecret(input, &node, hmacSecretSourceForAction(action)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_hmac_secret", err.Error())
		return
	}
	saved, err := s.store.UpsertNode(r.Context(), node)
	if err != nil {
		s.logger.Error("upsert node", "node_id", node.NodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", describeStoreError("保存节点失败", err))
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     action,
		TargetType: "node",
		TargetID:   saved.NodeID,
		Request:    redactedNodeInput(input),
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", action, "node_id", saved.NodeID, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": saved})
}

func (s *Server) applyNodeHMACSecret(input NodeInput, node *Node, source string) error {
	secret := strings.TrimSpace(input.HMACSecret)
	if secret == "" {
		return nil
	}
	if len(secret) < 16 {
		return errors.New("hmac_secret must be at least 16 characters")
	}
	if s.secrets == nil {
		return errors.New("secret box is not configured")
	}
	encrypted, err := s.secrets.Encrypt(secret)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	node.HMACSecretEncrypted = encrypted
	node.HMACSecretConfigured = true
	node.HMACSecretSource = strings.TrimSpace(source)
	if node.HMACSecretSource == "" {
		node.HMACSecretSource = "backend"
	}
	node.HMACSecretUpdatedAt = &now
	return nil
}

func hmacSecretSourceForAction(action string) string {
	if strings.HasPrefix(action, "backend.") {
		return "backend"
	}
	return "panel"
}

func redactedNodeInput(input NodeInput) map[string]any {
	raw, err := json.Marshal(input)
	if err != nil {
		return map[string]any{"node_id": input.NodeID}
	}
	payload := make(map[string]any)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{"node_id": input.NodeID}
	}
	if _, ok := payload["hmac_secret"]; ok {
		payload["hmac_secret"] = "[redacted]"
	}
	return payload
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request, nodeID string, action string) {
	if err := s.store.DeleteNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "node not found")
			return
		}
		s.logger.Error("delete node", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "delete node failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     action,
		TargetType: "node",
		TargetID:   nodeID,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", action, "node_id", nodeID, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleNodeReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	_ = r.Body.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	var payload NodeReportPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	report, err := s.store.SaveNodeReport(r.Context(), NodeReportInput{
		Payload: payload,
		RawJSON: raw,
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node is not registered in panel")
			return
		}
		s.logger.Error("save node report", "node_id", payload.Node.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "save report failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"report": report,
	})
}

func (s *Server) handleNodeCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node_id_required", "node_id query is required")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 10, 50)
	now := time.Now().UTC()
	tasks, err := s.store.ClaimPendingNodeTasks(r.Context(), nodeID, limit, now)
	if err != nil {
		s.logger.Error("claim node commands", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "claim commands failed")
		return
	}
	if len(tasks) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	commands := make([]panelcommand.Command, 0, len(tasks))
	for _, task := range tasks {
		commands = append(commands, nodeCommandFromTask(task, now, s.cfg.NodeCommand.DefaultExpiresIn))
	}
	body, err := json.Marshal(panelcommand.Envelope{Commands: commands})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal_error", "marshal commands failed")
		return
	}
	timestamp, nonce, signature, err := signCommandBody(s.cfg.NodeCommand.Secret, body, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sign_error", "sign commands failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(panelcommand.HeaderTimestamp, timestamp)
	w.Header().Set(panelcommand.HeaderNonce, nonce)
	w.Header().Set(panelcommand.HeaderSignature, signature)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleListNodeReports(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20, 100)
	reports, err := s.store.ListNodeReports(r.Context(), nodeID, limit)
	if err != nil {
		s.logger.Error("list node reports", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list reports failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reports": reports,
		"count":   len(reports),
	})
}

func (s *Server) handleListNodeTasks(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20, 100)
	tasks, err := s.store.ListNodeTasks(r.Context(), nodeID, limit)
	if err != nil {
		s.logger.Error("list node tasks", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list tasks failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	})
}

func (s *Server) handleListTaskLogs(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 200, 500)
	logs, err := s.store.ListNodeTaskLogs(r.Context(), taskID, limit)
	if err != nil {
		s.logger.Error("list task logs", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list task logs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logs":  logs,
		"count": len(logs),
	})
}

func (s *Server) handleCreateDeployTask(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.secrets == nil {
		writeError(w, http.StatusInternalServerError, "secret_box_unavailable", "secret box is not configured")
		return
	}
	var req DeployNodeTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input, err := NewDeployNodeTask(nodeID, req, s.cfg.Deploy.DefaultNodeVersion, time.Now().UTC(), s.secrets)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("get node for deploy", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get node failed")
		return
	}
	if strings.TrimSpace(req.HMACSecret) == "" && !node.HMACSecretConfigured {
		writeError(w, http.StatusBadRequest, "hmac_secret_required", "node hmac_secret is not configured; sync it from backend before deploy")
		return
	}
	if _, err := s.store.GetNodeCredential(r.Context(), nodeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusBadRequest, "credential_required", "node SSH credential is required before deploy")
			return
		}
		s.logger.Error("get node credential", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get credential failed")
		return
	}
	task, err := s.store.CreateNodeTask(r.Context(), input)
	if err != nil {
		s.logger.Error("create deploy task", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "create task failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.node.deploy",
		TargetType: "node",
		TargetID:   nodeID,
		Request: map[string]any{
			"version":        strings.TrimSpace(req.Version),
			"panel_base_url": strings.TrimSpace(req.PanelBaseURL),
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.node.deploy", "node_id", nodeID, "error", err)
	}
	go s.runDeployNodeTask(*task)
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleCreateUpdateTask(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req UpdateNodeTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input, err := NewUpdateNodeTask(nodeID, req, s.cfg.Deploy.DefaultNodeVersion, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	if _, err := s.store.GetNodeCredential(r.Context(), nodeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusBadRequest, "credential_required", "node SSH credential is required before update")
			return
		}
		s.logger.Error("get node credential", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get credential failed")
		return
	}
	task, err := s.store.CreateNodeTask(r.Context(), input)
	if err != nil {
		s.logger.Error("create update task", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "create task failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.node.update",
		TargetType: "node",
		TargetID:   nodeID,
		Request: map[string]any{
			"version": strings.TrimSpace(req.Version),
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.node.update", "node_id", nodeID, "error", err)
	}
	go s.runUpdateNodeTask(*task)
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleCreateApplyPolicyTask(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req ApplyPolicyTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input, err := NewApplyPolicyTask(nodeID, req, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	task, err := s.store.CreateNodeTask(r.Context(), input)
	if err != nil {
		s.logger.Error("create apply_policy task", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "create task failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.node.command.apply_policy",
		TargetType: "node",
		TargetID:   nodeID,
		Request:    req,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.node.command.apply_policy", "node_id", nodeID, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) authorizeAPIKey(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if provided == "" {
		return false
	}
	for _, key := range s.cfg.Security.BackendAPIKeys {
		if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid json: multiple JSON values")
	}
	return nil
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if idx := strings.IndexByte(forwarded, ','); idx >= 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return forwarded
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var panelErrorMessageText = map[string]string{
	"method not allowed":                                            "请求方法不允许",
	"permission denied":                                             "当前账号没有执行该操作的权限",
	"login required":                                                "请先登录控制面板",
	"missing or invalid API key":                                    "API Key 缺失或无效",
	"node store is not configured":                                  "节点存储未配置",
	"username or password is invalid":                               "账号或密码不正确",
	"create session failed":                                         "创建登录会话失败",
	"refresh session failed":                                        "刷新登录会话失败",
	"current password is invalid":                                   "当前密码不正确",
	"new password must be different from current password":          "新密码不能与当前密码相同",
	"hash password failed":                                          "密码加密失败",
	"update password failed":                                        "更新密码失败",
	"panel user not found":                                          "面板账号不存在",
	"panel user already exists":                                     "面板账号已存在",
	"admin cannot demote or disable itself":                         "管理员不能降低或禁用自己的权限",
	"use /api/panel/me/password to change your own password":        "请在当前账号入口修改自己的密码",
	"list nodes failed":                                             "读取节点列表失败",
	"get node failed":                                               "读取节点失败",
	"node not found":                                                "节点不存在",
	"node is not registered in panel":                               "节点尚未在控制面板登记",
	"body node_id does not match path node_id":                      "请求体里的节点 ID 与地址里的节点 ID 不一致",
	"upsert node failed":                                            "保存节点失败",
	"delete node failed":                                            "删除节点失败",
	"save report failed":                                            "保存节点上报失败",
	"claim commands failed":                                         "获取节点命令失败",
	"marshal commands failed":                                       "生成节点命令失败",
	"sign commands failed":                                          "签名节点命令失败",
	"list reports failed":                                           "读取节点上报记录失败",
	"list tasks failed":                                             "读取任务列表失败",
	"list task logs failed":                                         "读取任务日志失败",
	"create task failed":                                            "创建任务失败",
	"create policy task failed":                                     "创建策略任务失败",
	"set desired policy failed":                                     "设置目标策略失败",
	"get credential failed":                                         "读取 SSH 凭据失败",
	"save credential failed":                                        "保存 SSH 凭据失败",
	"delete credential failed":                                      "删除 SSH 凭据失败",
	"credential not found":                                          "SSH 凭据不存在",
	"node SSH credential is required before deploy":                 "部署前需要先保存节点 SSH 凭据",
	"node SSH credential is required before update":                 "更新前需要先保存节点 SSH 凭据",
	"node SSH credential is required before repairing admin access": "修复 Admin 接入前需要先保存节点 SSH 凭据",
	"node SSH credential is required before tuning UDP buffer":      "优化 UDP Buffer 前需要先保存节点 SSH 凭据",
	"node hmac_secret is not configured; sync it from backend before deploy": "节点 HMAC Secret 尚未从业务后台同步，无法部署",
	"secret box is not configured":                                           "密钥加密组件未配置",
	"get token defaults failed":                                              "读取客户端默认配置失败",
}

var panelErrorCodeText = map[string]string{
	"request_failed":           "请求失败",
	"method_not_allowed":       "请求方法不允许",
	"unauthorized":             "登录已过期或鉴权失败",
	"forbidden":                "当前账号没有执行该操作的权限",
	"login_required":           "请先登录控制面板",
	"invalid_credentials":      "账号或密码不正确",
	"login_rate_limited":       "登录尝试过于频繁，请稍后再试",
	"invalid_json":             "请求 JSON 格式错误",
	"invalid_body":             "请求内容格式错误",
	"invalid_node":             "节点配置不合法",
	"invalid_hmac_secret":      "节点 HMAC Secret 不合法",
	"node_id_mismatch":         "节点 ID 不一致",
	"node_not_found":           "节点不存在",
	"not_found":                "数据不存在",
	"store_error":              "数据库操作失败",
	"store_unavailable":        "数据库存储未配置",
	"session_error":            "登录会话处理失败",
	"invalid_current_password": "当前密码不正确",
	"weak_password":            "新密码强度不足",
	"password_reused":          "新密码不能与当前密码相同",
	"password_hash_error":      "密码加密失败",
	"invalid_user":             "账号信息不合法",
	"invalid_user_id":          "账号 ID 不合法",
	"user_exists":              "账号已存在",
	"cannot_modify_self":       "不能降低或禁用自己的账号",
	"use_self_password_change": "请在当前账号入口修改自己的密码",
	"credential_required":      "需要先保存节点 SSH 凭据",
	"credential_not_found":     "SSH 凭据不存在",
	"secret_box_unavailable":   "密钥加密组件不可用",
	"invalid_credential":       "SSH 凭据不合法",
	"invalid_task":             "任务参数不合法",
	"invalid_limit":            "分页数量不合法",
	"invalid_offset":           "分页偏移不合法",
	"invalid_policy_revision":  "策略版本不合法",
	"node_id_required":         "缺少节点 ID",
	"hmac_secret_required":     "节点 HMAC Secret 尚未配置",
	"marshal_error":            "数据序列化失败",
	"sign_error":               "签名失败",
	"invalid_token_defaults":   "客户端默认配置不合法",
}

func panelErrorText(code string, message string) string {
	message = strings.TrimSpace(message)
	if translated, ok := panelErrorMessageText[message]; ok {
		return translated
	}
	prefix, ok := panelErrorCodeText[code]
	if !ok {
		return message
	}
	if message == "" {
		return prefix
	}
	if containsCJK(message) {
		return message
	}
	if strings.HasPrefix(code, "invalid_") || code == "weak_password" {
		return prefix + "：" + message
	}
	return prefix
}

func containsCJK(value string) bool {
	for _, r := range value {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func describeStoreError(action string, err error) string {
	if err == nil {
		return action
	}
	detail := strings.TrimSpace(err.Error())
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "unknown column"):
		if column := quotedMySQLName(detail); column != "" {
			return action + "：数据库缺少字段 " + column + "，请执行对应迁移 SQL"
		}
		return action + "：数据库字段缺失，请执行最新迁移 SQL"
	case strings.Contains(lower, "doesn't exist") || strings.Contains(lower, "does not exist"):
		return action + "：数据库表不存在，请先导入 panel_schema.sql"
	case strings.Contains(lower, "invalid json"):
		return action + "：JSON 字段格式不合法，请检查标签和扩展字段"
	case strings.Contains(lower, "data truncated for column"):
		if column := quotedMySQLName(detail); column != "" {
			return action + "：字段 " + column + " 的值不符合数据库枚举或类型"
		}
		return action + "：字段值不符合数据库枚举或类型"
	case strings.Contains(lower, "data too long for column"):
		if column := quotedMySQLName(detail); column != "" {
			return action + "：字段 " + column + " 内容过长"
		}
		return action + "：字段内容过长"
	case strings.Contains(lower, "duplicate entry"):
		return action + "：节点 ID 已存在，请换一个节点 ID"
	case strings.Contains(lower, "access denied"):
		return action + "：数据库账号权限不足，请检查 panel.yaml 的 DSN"
	default:
		return action + "：" + detail
	}
}

func quotedMySQLName(message string) string {
	start := strings.IndexByte(message, '\'')
	if start < 0 {
		return ""
	}
	rest := message[start+1:]
	end := strings.IndexByte(rest, '\'')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": panelErrorText(code, message),
		},
	})
}

func parsePositiveInt(value string, fallback int, max int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	if max > 0 && parsed > max {
		return max
	}
	return parsed
}
