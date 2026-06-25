package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	tuneUDPBufferOperation      = "tune_udp_buffer"
	defaultQUICUDPBufferBytes   = recommendedUDPBufferBytes
	minQUICUDPBufferBytes       = 7_500_000
	maxQUICUDPBufferBytes       = 128 * 1024 * 1024
	quicUDPBufferSysctlFile     = "/etc/sysctl.d/99-gaccel-quic.conf"
	quicUDPBufferWarningSnippet = "failed to sufficiently increase receive buffer size"
)

type TuneUDPBufferTaskRequest struct {
	Operation          string `json:"operation,omitempty"`
	ReceiveBufferBytes int    `json:"receive_buffer_bytes,omitempty"`
	SendBufferBytes    int    `json:"send_buffer_bytes,omitempty"`
	Priority           int    `json:"priority,omitempty"`
}

type TuneUDPBufferTaskResult struct {
	NodeID                  string `json:"node_id"`
	ConfigFile              string `json:"config_file"`
	BackupDir               string `json:"backup_dir,omitempty"`
	ReceiveBufferBytes      int    `json:"receive_buffer_bytes"`
	SendBufferBytes         int    `json:"send_buffer_bytes"`
	AppliedReceiveBytes     int    `json:"applied_receive_bytes,omitempty"`
	AppliedSendBytes        int    `json:"applied_send_bytes,omitempty"`
	AppliedReceiveDefault   int    `json:"applied_receive_default,omitempty"`
	AppliedSendDefault      int    `json:"applied_send_default,omitempty"`
	AppliedNetdevBacklog    int    `json:"applied_netdev_backlog,omitempty"`
	LocalHealthOK           bool   `json:"local_health_ok"`
	LocalStatusOK           bool   `json:"local_status_ok"`
	BufferWarningNotPresent bool   `json:"buffer_warning_not_present"`
}

func NewTuneUDPBufferTask(nodeID string, req TuneUDPBufferTaskRequest, now time.Time) (NodeTaskInput, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodeTaskInput{}, errors.New("node_id is required")
	}
	req = normalizeTuneUDPBufferRequest(req)
	if err := validateTuneUDPBufferRequest(req); err != nil {
		return NodeTaskInput{}, err
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 35
	}
	taskID, err := newTaskID("tune_udp_buffer", now)
	if err != nil {
		return NodeTaskInput{}, err
	}
	return NodeTaskInput{
		TaskID:   taskID,
		NodeID:   nodeID,
		Type:     TaskTypeRestartNode,
		Status:   TaskStatusPending,
		Priority: priority,
		RequestJSON: TuneUDPBufferTaskRequest{
			Operation:          tuneUDPBufferOperation,
			ReceiveBufferBytes: req.ReceiveBufferBytes,
			SendBufferBytes:    req.SendBufferBytes,
		},
	}, nil
}

func normalizeTuneUDPBufferRequest(req TuneUDPBufferTaskRequest) TuneUDPBufferTaskRequest {
	req.Operation = strings.TrimSpace(req.Operation)
	if req.Operation == "" {
		req.Operation = tuneUDPBufferOperation
	}
	if req.ReceiveBufferBytes <= 0 {
		req.ReceiveBufferBytes = defaultQUICUDPBufferBytes
	}
	if req.SendBufferBytes <= 0 {
		req.SendBufferBytes = defaultQUICUDPBufferBytes
	}
	return req
}

func validateTuneUDPBufferRequest(req TuneUDPBufferTaskRequest) error {
	if req.Operation != tuneUDPBufferOperation {
		return fmt.Errorf("unsupported restart_node operation %q", req.Operation)
	}
	if req.ReceiveBufferBytes < minQUICUDPBufferBytes || req.ReceiveBufferBytes > maxQUICUDPBufferBytes {
		return fmt.Errorf("receive_buffer_bytes must be between %d and %d", minQUICUDPBufferBytes, maxQUICUDPBufferBytes)
	}
	if req.SendBufferBytes < minQUICUDPBufferBytes || req.SendBufferBytes > maxQUICUDPBufferBytes {
		return fmt.Errorf("send_buffer_bytes must be between %d and %d", minQUICUDPBufferBytes, maxQUICUDPBufferBytes)
	}
	return nil
}

func isTuneUDPBufferTask(task NodeTask) bool {
	req, err := tuneUDPBufferRequestFromTask(task)
	return err == nil && req.Operation == tuneUDPBufferOperation
}

func tuneUDPBufferRequestFromTask(task NodeTask) (TuneUDPBufferTaskRequest, error) {
	var req TuneUDPBufferTaskRequest
	if len(task.RequestJSON) == 0 {
		return req, errors.New("tune udp buffer task request is empty")
	}
	if err := json.Unmarshal(task.RequestJSON, &req); err != nil {
		return req, fmt.Errorf("decode tune udp buffer request: %w", err)
	}
	req = normalizeTuneUDPBufferRequest(req)
	if err := validateTuneUDPBufferRequest(req); err != nil {
		return req, err
	}
	return req, nil
}

func (s *Server) handleCreateTuneUDPBufferTask(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req TuneUDPBufferTaskRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input, err := NewTuneUDPBufferTask(nodeID, req, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	if _, err := s.store.GetNodeCredential(r.Context(), nodeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusBadRequest, "credential_required", "node SSH credential is required before tuning UDP buffer")
			return
		}
		s.logger.Error("get node credential", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get credential failed")
		return
	}
	task, err := s.store.CreateNodeTask(r.Context(), input)
	if err != nil {
		s.logger.Error("create tune udp buffer task", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "create task failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.node.tune_udp_buffer",
		TargetType: "node",
		TargetID:   nodeID,
		Request: map[string]any{
			"receive_buffer_bytes": input.RequestJSON.(TuneUDPBufferTaskRequest).ReceiveBufferBytes,
			"send_buffer_bytes":    input.RequestJSON.(TuneUDPBufferTaskRequest).SendBufferBytes,
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.node.tune_udp_buffer", "node_id", nodeID, "error", err)
	}
	go s.runTuneUDPBufferTask(*task)
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) runTuneUDPBufferTask(task NodeTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{TaskID: task.TaskID, Status: TaskStatusRunning, StartedAt: &now})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "deploying", "", "")
	logger := deployLogger{server: s, task: task}
	logger.info("start", "tune UDP buffer task started")

	result, err := s.tuneUDPBuffer(ctx, task, logger)
	finishedAt := time.Now().UTC()
	if err != nil {
		logger.error("failed", err.Error())
		_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{
			TaskID:       task.TaskID,
			Status:       TaskStatusFailed,
			ResultJSON:   result,
			ErrorMessage: err.Error(),
			FinishedAt:   &finishedAt,
		})
		_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "error", "", err.Error())
		return
	}
	logger.info("done", "tune UDP buffer task completed")
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{
		TaskID:     task.TaskID,
		Status:     TaskStatusSuccess,
		ResultJSON: result,
		FinishedAt: &finishedAt,
	})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "online", "", "")
}

func (s *Server) tuneUDPBuffer(ctx context.Context, task NodeTask, logger deployLogger) (TuneUDPBufferTaskResult, error) {
	req, err := tuneUDPBufferRequestFromTask(task)
	if err != nil {
		return TuneUDPBufferTaskResult{}, err
	}
	node, err := s.store.GetNode(ctx, task.NodeID)
	if err != nil {
		return TuneUDPBufferTaskResult{}, err
	}
	credential, err := s.store.GetNodeCredential(ctx, task.NodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return TuneUDPBufferTaskResult{}, errors.New("node SSH credential is not configured")
		}
		return TuneUDPBufferTaskResult{}, err
	}
	client, err := s.openSSHClient(ctx, *node, *credential)
	if err != nil {
		return TuneUDPBufferTaskResult{}, err
	}
	defer client.Close()
	_ = s.store.MarkNodeCredentialUsed(ctx, task.NodeID, time.Now().UTC())

	runner := sshRunner{
		client:  client,
		timeout: s.cfg.Deploy.CommandTimeout,
		root:    credential.SudoMode == CredentialSudoSudo,
	}
	port := node.AdminPort
	if port <= 0 {
		port = 5557
	}
	localAdminURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	result := TuneUDPBufferTaskResult{
		NodeID:             node.NodeID,
		ConfigFile:         quicUDPBufferSysctlFile,
		ReceiveBufferBytes: req.ReceiveBufferBytes,
		SendBufferBytes:    req.SendBufferBytes,
	}

	logger.info("precheck", "checking target system")
	precheck := `command -v sh >/dev/null && command -v sysctl >/dev/null && command -v systemctl >/dev/null && command -v install >/dev/null && command -v cp >/dev/null && command -v cat >/dev/null && command -v curl >/dev/null && command -v journalctl >/dev/null`
	if _, err := runner.run(ctx, "precheck", precheck, logger); err != nil {
		return result, fmt.Errorf("precheck failed: %w", err)
	}

	backupDir := "/var/lib/gaccel-node/backups/" + task.TaskID
	result.BackupDir = backupDir
	startedUnix := time.Now().Unix()
	logger.info("sysctl", "writing QUIC UDP buffer sysctl config")
	applyCmd := fmt.Sprintf(`set -eu
config=%s
backup_dir=%s
install -d -m 0750 -o root -g root "$backup_dir"
if [ -f "$config" ]; then
  cp -p "$config" "$backup_dir/99-gaccel-quic.conf"
fi
cat > "$config" <<'GACCEL_QUIC_SYSCTL'
# Managed by gaccel-panel. QUIC uses UDP; keep socket buffer ceilings high enough for bursty game traffic.
net.core.rmem_max=%d
net.core.wmem_max=%d
net.core.rmem_default=%d
net.core.wmem_default=%d
net.core.netdev_max_backlog=%d
GACCEL_QUIC_SYSCTL
chmod 0644 "$config"
sysctl -p "$config"
rmem=$(sysctl -n net.core.rmem_max)
wmem=$(sysctl -n net.core.wmem_max)
rmem_default=$(sysctl -n net.core.rmem_default)
wmem_default=$(sysctl -n net.core.wmem_default)
netdev_backlog=$(sysctl -n net.core.netdev_max_backlog)
echo "applied net.core.rmem_max=$rmem"
echo "applied net.core.wmem_max=$wmem"
echo "applied net.core.rmem_default=$rmem_default"
echo "applied net.core.wmem_default=$wmem_default"
echo "applied net.core.netdev_max_backlog=$netdev_backlog"
test "$rmem" -ge %d
test "$wmem" -ge %d
test "$rmem_default" -ge %d
test "$wmem_default" -ge %d
test "$netdev_backlog" -ge %d
systemctl restart gaccel-node`, shellQuote(quicUDPBufferSysctlFile), shellQuote(backupDir), req.ReceiveBufferBytes, req.SendBufferBytes, recommendedUDPDefaultBufferBytes, recommendedUDPDefaultBufferBytes, recommendedNetworkDeviceBacklog, req.ReceiveBufferBytes, req.SendBufferBytes, recommendedUDPDefaultBufferBytes, recommendedUDPDefaultBufferBytes, recommendedNetworkDeviceBacklog)
	if _, err := runner.runRoot(ctx, "sysctl", applyCmd, logger); err != nil {
		return result, fmt.Errorf("tune sysctl failed: %w", err)
	}

	valuesOutput, err := runner.runRoot(ctx, "sysctl-read", `printf "rmem=%s\n" "$(sysctl -n net.core.rmem_max)"
printf "wmem=%s\n" "$(sysctl -n net.core.wmem_max)"
printf "rmem_default=%s\n" "$(sysctl -n net.core.rmem_default)"
printf "wmem_default=%s\n" "$(sysctl -n net.core.wmem_default)"
printf "netdev_backlog=%s\n" "$(sysctl -n net.core.netdev_max_backlog)"`, logger)
	if err != nil {
		return result, fmt.Errorf("read sysctl values failed: %w", err)
	}
	result.AppliedReceiveBytes = parseNamedSysctlValue(valuesOutput, "rmem")
	result.AppliedSendBytes = parseNamedSysctlValue(valuesOutput, "wmem")
	result.AppliedReceiveDefault = parseNamedSysctlValue(valuesOutput, "rmem_default")
	result.AppliedSendDefault = parseNamedSysctlValue(valuesOutput, "wmem_default")
	result.AppliedNetdevBacklog = parseNamedSysctlValue(valuesOutput, "netdev_backlog")

	logger.info("verify", "checking node local admin health and status")
	if _, err := runner.runRoot(ctx, "verify-health", waitForLocalAdminCommand(localAdminURL+"/health", port), logger); err != nil {
		return result, fmt.Errorf("local health check failed: %w", err)
	}
	result.LocalHealthOK = true
	if _, err := runner.runRoot(ctx, "verify-status", waitForLocalAdminCommand(localAdminURL+"/status", port), logger); err != nil {
		return result, fmt.Errorf("local status check failed: %w", err)
	}
	result.LocalStatusOK = true

	logger.info("verify-journal", "checking recent gaccel-node UDP buffer warning")
	checkWarningCmd := fmt.Sprintf(`if journalctl -u gaccel-node --since @%d --no-pager | grep -F %s; then
  exit 9
fi`, startedUnix, shellQuote(quicUDPBufferWarningSnippet))
	if _, err := runner.runRoot(ctx, "verify-journal", checkWarningCmd, logger); err != nil {
		return result, fmt.Errorf("UDP buffer warning still present after restart: %w", err)
	}
	result.BufferWarningNotPresent = true
	return result, nil
}

func parseNamedSysctlValue(output string, key string) int {
	prefix := key + "="
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			value, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			return value
		}
	}
	return 0
}
