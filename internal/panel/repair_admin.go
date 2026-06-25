package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const repairAdminOperation = "repair_admin_listen"

type RepairAdminTaskRequest struct {
	Operation  string `json:"operation,omitempty"`
	ListenHost string `json:"listen_host,omitempty"`
	Priority   int    `json:"priority,omitempty"`
}

type RepairAdminTaskResult struct {
	NodeID          string `json:"node_id"`
	Listen          string `json:"listen"`
	BackupDir       string `json:"backup_dir,omitempty"`
	AdminURL        string `json:"admin_url"`
	LocalHealthOK   bool   `json:"local_health_ok"`
	LocalStatusOK   bool   `json:"local_status_ok"`
	ExternalProbeOK bool   `json:"external_probe_ok"`
}

func NewRepairAdminTask(nodeID string, req RepairAdminTaskRequest, now time.Time) (NodeTaskInput, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodeTaskInput{}, errors.New("node_id is required")
	}
	listenHost := strings.TrimSpace(req.ListenHost)
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}
	if err := validateAdminListenHost(listenHost); err != nil {
		return NodeTaskInput{}, err
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 40
	}
	taskID, err := newTaskID("repair_admin", now)
	if err != nil {
		return NodeTaskInput{}, err
	}
	return NodeTaskInput{
		TaskID:   taskID,
		NodeID:   nodeID,
		Type:     TaskTypeRestartNode,
		Status:   TaskStatusPending,
		Priority: priority,
		RequestJSON: RepairAdminTaskRequest{
			Operation:  repairAdminOperation,
			ListenHost: listenHost,
		},
	}, nil
}

func validateAdminListenHost(host string) error {
	switch strings.TrimSpace(host) {
	case "0.0.0.0", "127.0.0.1", "::", "::1":
		return nil
	default:
		return errors.New("listen_host must be 0.0.0.0, 127.0.0.1, ::, or ::1")
	}
}

func isRepairAdminTask(task NodeTask) bool {
	req, err := repairAdminRequestFromTask(task)
	return err == nil && req.Operation == repairAdminOperation
}

func repairAdminRequestFromTask(task NodeTask) (RepairAdminTaskRequest, error) {
	var req RepairAdminTaskRequest
	if len(task.RequestJSON) == 0 {
		return req, errors.New("repair admin task request is empty")
	}
	if err := json.Unmarshal(task.RequestJSON, &req); err != nil {
		return req, fmt.Errorf("decode repair admin request: %w", err)
	}
	req.Operation = strings.TrimSpace(req.Operation)
	req.ListenHost = strings.TrimSpace(req.ListenHost)
	if req.Operation == "" {
		req.Operation = repairAdminOperation
	}
	if req.ListenHost == "" {
		req.ListenHost = "0.0.0.0"
	}
	if req.Operation != repairAdminOperation {
		return req, fmt.Errorf("unsupported restart_node operation %q", req.Operation)
	}
	if err := validateAdminListenHost(req.ListenHost); err != nil {
		return req, err
	}
	return req, nil
}

func (s *Server) handleCreateRepairAdminTask(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req RepairAdminTaskRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input, err := NewRepairAdminTask(nodeID, req, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	if _, err := s.store.GetNodeCredential(r.Context(), nodeID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusBadRequest, "credential_required", "node SSH credential is required before repairing admin access")
			return
		}
		s.logger.Error("get node credential", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get credential failed")
		return
	}
	task, err := s.store.CreateNodeTask(r.Context(), input)
	if err != nil {
		s.logger.Error("create repair admin task", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "create task failed")
		return
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.node.repair_admin",
		TargetType: "node",
		TargetID:   nodeID,
		Request: map[string]any{
			"listen_host": strings.TrimSpace(req.ListenHost),
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.node.repair_admin", "node_id", nodeID, "error", err)
	}
	go s.runRepairAdminTask(*task)
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) runRepairAdminTask(task NodeTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{TaskID: task.TaskID, Status: TaskStatusRunning, StartedAt: &now})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "deploying", "", "")
	logger := deployLogger{server: s, task: task}
	logger.info("start", "repair admin access task started")

	result, err := s.repairAdmin(ctx, task, logger)
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
	logger.info("done", "repair admin access task completed")
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{
		TaskID:     task.TaskID,
		Status:     TaskStatusSuccess,
		ResultJSON: result,
		FinishedAt: &finishedAt,
	})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "online", "", "")
}

func (s *Server) repairAdmin(ctx context.Context, task NodeTask, logger deployLogger) (RepairAdminTaskResult, error) {
	req, err := repairAdminRequestFromTask(task)
	if err != nil {
		return RepairAdminTaskResult{}, err
	}
	node, err := s.store.GetNode(ctx, task.NodeID)
	if err != nil {
		return RepairAdminTaskResult{}, err
	}
	credential, err := s.store.GetNodeCredential(ctx, task.NodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return RepairAdminTaskResult{}, errors.New("node SSH credential is not configured")
		}
		return RepairAdminTaskResult{}, err
	}
	client, err := s.openSSHClient(ctx, *node, *credential)
	if err != nil {
		return RepairAdminTaskResult{}, err
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
	listen := fmt.Sprintf("%s:%d", req.ListenHost, port)
	localAdminURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	result := RepairAdminTaskResult{
		NodeID:   node.NodeID,
		Listen:   listen,
		AdminURL: nodeAdminBaseURL(*node),
	}

	logger.info("precheck", "checking target system")
	precheck := `command -v sh >/dev/null && command -v awk >/dev/null && command -v systemctl >/dev/null && command -v cp >/dev/null && command -v install >/dev/null && command -v mktemp >/dev/null && command -v curl >/dev/null`
	if _, err := runner.run(ctx, "precheck", precheck, logger); err != nil {
		return result, fmt.Errorf("precheck failed: %w", err)
	}

	backupDir := "/var/lib/gaccel-node/backups/" + task.TaskID
	result.BackupDir = backupDir
	logger.info("backup", "backing up /etc/gaccel-node/config.yaml")
	repairCmd := fmt.Sprintf(`set -eu
config=/etc/gaccel-node/config.yaml
test -f "$config"
backup_dir=%s
install -d -m 0750 -o root -g root "$backup_dir"
cp -p "$config" "$backup_dir/config.yaml"
tmp=$(mktemp)
awk -v listen=%s '
BEGIN { in_admin=0; wrote=0 }
$0 ~ /^admin:[[:space:]]*$/ { print "admin:"; print "  listen: \"" listen "\""; in_admin=1; wrote=1; next }
in_admin && $0 ~ /^[^[:space:]][^:]*:/ { in_admin=0 }
in_admin && $0 ~ /^[[:space:]]+listen:[[:space:]]*/ { next }
{ print }
END { if (wrote == 0) { print ""; print "admin:"; print "  listen: \"" listen "\"" } }
' "$config" > "$tmp"
cp "$tmp" "$config"
rm -f "$tmp"
chown root:gaccel "$config" 2>/dev/null || chown root:root "$config"
chmod 0640 "$config" 2>/dev/null || chmod 0600 "$config"
systemctl restart gaccel-node`, shellQuote(backupDir), shellQuote(listen))
	if _, err := runner.runRoot(ctx, "repair-config", repairCmd, logger); err != nil {
		return result, fmt.Errorf("repair config failed: %w", err)
	}

	logger.info("verify", "checking node local admin health and status")
	if _, err := runner.runRoot(ctx, "verify-health", waitForLocalAdminCommand(localAdminURL+"/health", port), logger); err != nil {
		return result, fmt.Errorf("local health check failed: %w", err)
	}
	result.LocalHealthOK = true
	if _, err := runner.runRoot(ctx, "verify-status", waitForLocalAdminCommand(localAdminURL+"/status", port), logger); err != nil {
		return result, fmt.Errorf("local status check failed: %w", err)
	}
	result.LocalStatusOK = true

	logger.info("verify-external", "checking panel-side admin reachability")
	clientHTTP := &http.Client{Timeout: 4 * time.Second}
	var probeErr error
	for attempt := 1; attempt <= 3; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		_, probeErr = probeAdminJSON(probeCtx, clientHTTP, result.AdminURL+"/health")
		cancel()
		if probeErr == nil {
			result.ExternalProbeOK = true
			return result, nil
		}
		if attempt < 3 {
			time.Sleep(time.Second)
		}
	}
	return result, fmt.Errorf("panel-side admin probe failed: %w", probeErr)
}

func waitForLocalAdminCommand(url string, adminPort int) string {
	return fmt.Sprintf(`for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS --connect-timeout 1 --max-time 2 %s; then
    exit 0
  fi
  sleep 1
done
echo "--- gaccel-node service status ---"
systemctl status gaccel-node --no-pager -l || true
echo "--- gaccel-node recent journal ---"
journalctl -u gaccel-node -n 80 --no-pager || true
echo "--- listening tcp ports ---"
if command -v ss >/dev/null 2>&1; then
  ss -ltnp | grep -E ':(5555|%d)' || true
else
  netstat -ltnp 2>/dev/null | grep -E ':(5555|%d)' || true
fi
exit 7`, shellQuote(url), adminPort, adminPort)
}
