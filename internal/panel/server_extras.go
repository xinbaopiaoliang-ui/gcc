package panel

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type RetryTaskRequest struct {
	Priority int `json:"priority,omitempty"`
}

type BackendAPIKeyView struct {
	Index  int    `json:"index"`
	Key    string `json:"key"`
	Masked string `json:"masked"`
	Length int    `json:"length"`
}

type BackendAPIKeysResponse struct {
	Count int                 `json:"count"`
	Keys  []BackendAPIKeyView `json:"keys"`
}

func (s *Server) handlePanelPolicyValidation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanelAdmin(w, r); !ok {
		return
	}
	s.handlePolicyValidation(w, r)
}

func (s *Server) handleBackendPolicyValidation(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	s.handlePolicyValidation(w, r)
}

func (s *Server) handlePolicyValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req PolicyValidationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.BaseRevision) != "" && strings.TrimSpace(req.BaseRoutePoliciesYAML) == "" {
		if s.store == nil {
			writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
			return
		}
		basePolicy, err := s.store.GetPolicyRevision(r.Context(), strings.TrimSpace(req.BaseRevision))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "base_policy_not_found", "base policy revision not found")
				return
			}
			s.logger.Error("get base policy revision", "revision", req.BaseRevision, "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "get base policy failed")
			return
		}
		req.BaseRoutePoliciesYAML = basePolicy.RoutePoliciesYAML
	}
	result := ValidatePolicyPackage(req)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetNodeSyncStatus(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if !nodeIDPattern.MatchString(nodeID) {
		writeError(w, http.StatusBadRequest, "invalid_node_id", "node_id is invalid")
		return
	}
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("get node sync status", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get node failed")
		return
	}
	reports, err := s.store.ListNodeReports(r.Context(), nodeID, 5)
	if err != nil {
		s.logger.Error("list node reports for sync status", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list reports failed")
		return
	}
	tasks, err := s.store.ListNodeTasks(r.Context(), nodeID, 20)
	if err != nil {
		s.logger.Error("list node tasks for sync status", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list tasks failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sync_status": BuildNodeSyncStatus(*node, reports, tasks, time.Now().UTC()),
	})
}

func (s *Server) handlePanelSecurityOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanelAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	users, err := s.store.ListPanelUsers(r.Context())
	if err != nil {
		s.logger.Error("list users for security overview", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list users failed")
		return
	}
	nodes, err := s.store.ListNodes(r.Context(), NodeListFilter{Limit: 10000})
	if err != nil {
		s.logger.Error("list nodes for security overview", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list nodes failed")
		return
	}
	overview := BuildSecurityOverview(s.cfg, users, nodes)
	for _, node := range nodes {
		if _, err := s.store.GetNodeCredential(r.Context(), node.NodeID); err == nil {
			overview.Nodes.WithCredentials++
		} else if errors.Is(err, ErrNotFound) {
			overview.Nodes.WithoutCredentials++
		}
	}
	if overview.Nodes.WithoutCredentials > 0 {
		overview.Warnings = append(overview.Warnings, "存在未保存 SSH 凭据的节点，无法从面板执行部署或更新")
	}
	writeJSON(w, http.StatusOK, map[string]any{"security": overview})
}

func (s *Server) handlePanelBackendAPIKeys(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePanelAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	keys := make([]BackendAPIKeyView, 0, len(s.cfg.Security.BackendAPIKeys))
	for i, key := range s.cfg.Security.BackendAPIKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, BackendAPIKeyView{
			Index:  i + 1,
			Key:    key,
			Masked: maskBackendAPIKey(key),
			Length: len(key),
		})
	}
	if s.store != nil {
		if err := s.store.RecordAudit(r.Context(), AuditLog{
			OperatorID: &user.ID,
			Action:     "panel.security.backend_api_keys.view",
			TargetType: "panel_config",
			TargetID:   "security.backend_api_keys",
			Request: map[string]any{
				"count": len(keys),
			},
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		}); err != nil {
			s.logger.Warn("record audit", "action", "panel.security.backend_api_keys.view", "error", err)
		}
	}
	writeJSON(w, http.StatusOK, BackendAPIKeysResponse{Count: len(keys), Keys: keys})
}

func maskBackendAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", 8) + key[len(key)-4:]
}

func (s *Server) handleRetryTask(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req RetryTaskRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	oldTask, err := s.store.GetNodeTask(r.Context(), strings.TrimSpace(taskID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "task_not_found", "task not found")
			return
		}
		s.logger.Error("get retry task", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get task failed")
		return
	}
	if !isRetryableTaskStatus(oldTask.Status) {
		writeError(w, http.StatusConflict, "task_not_retryable", "only success, failed, or cancelled tasks can be retried")
		return
	}
	now := time.Now().UTC()
	newTaskID, err := newTaskID(oldTask.Type+"-retry", now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "task_id_error", "create retry task id failed")
		return
	}
	priority := req.Priority
	if priority <= 0 {
		priority = oldTask.Priority
	}
	input := NodeTaskInput{
		TaskID:      newTaskID,
		NodeID:      oldTask.NodeID,
		Type:        oldTask.Type,
		Status:      TaskStatusPending,
		Priority:    priority,
		RequestJSON: oldTask.RequestJSON,
	}
	newTask, err := s.store.CreateNodeTask(r.Context(), input)
	if err != nil {
		s.logger.Error("create retry task", "old_task_id", oldTask.TaskID, "node_id", oldTask.NodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "create retry task failed")
		return
	}
	_, _ = s.store.AppendNodeTaskLog(r.Context(), NodeTaskLogInput{
		TaskID:  newTask.TaskID,
		NodeID:  newTask.NodeID,
		Step:    "retry",
		Stream:  "info",
		Message: "retry created from task " + oldTask.TaskID,
	})
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.task.retry",
		TargetType: "task",
		TargetID:   oldTask.TaskID,
		Request: map[string]any{
			"new_task_id": newTask.TaskID,
			"node_id":     newTask.NodeID,
			"type":        newTask.Type,
			"priority":    newTask.Priority,
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.task.retry", "task_id", oldTask.TaskID, "error", err)
	}
	switch newTask.Type {
	case TaskTypeDeployNode:
		go s.runDeployNodeTask(*newTask)
	case TaskTypeUpdateNode:
		go s.runUpdateNodeTask(*newTask)
	case TaskTypeRestartNode:
		if isRepairAdminTask(*newTask) {
			go s.runRepairAdminTask(*newTask)
		} else if isTuneUDPBufferTask(*newTask) {
			go s.runTuneUDPBufferTask(*newTask)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":        newTask,
		"source_task": oldTask,
	})
}

func isRetryableTaskStatus(status string) bool {
	switch status {
	case TaskStatusSuccess, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func decodeOptionalJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}
