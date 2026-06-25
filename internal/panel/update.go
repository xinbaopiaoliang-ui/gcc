package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type UpdateNodeTaskRequest struct {
	Version  string `json:"version,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

type UpdateNodeTaskResult struct {
	Version     string `json:"version"`
	NodeID      string `json:"node_id"`
	BackupDir   string `json:"backup_dir,omitempty"`
	AdminURL    string `json:"admin_url"`
	HealthOK    bool   `json:"health_ok"`
	StatusOK    bool   `json:"status_ok"`
	NodeVersion string `json:"node_version,omitempty"`
	RolledBack  bool   `json:"rolled_back"`
}

func NewUpdateNodeTask(nodeID string, req UpdateNodeTaskRequest, defaultVersion string, now time.Time) (NodeTaskInput, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodeTaskInput{}, errors.New("node_id is required")
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = strings.TrimSpace(defaultVersion)
	}
	if version == "" {
		version = "latest"
	}
	version = normalizeNodeReleaseVersion(version)
	priority := req.Priority
	if priority <= 0 {
		priority = 50
	}
	taskID, err := newTaskID(TaskTypeUpdateNode, now)
	if err != nil {
		return NodeTaskInput{}, err
	}
	return NodeTaskInput{
		TaskID:   taskID,
		NodeID:   nodeID,
		Type:     TaskTypeUpdateNode,
		Status:   TaskStatusPending,
		Priority: priority,
		RequestJSON: UpdateNodeTaskRequest{
			Version: version,
		},
	}, nil
}

func (s *Server) runUpdateNodeTask(task NodeTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{TaskID: task.TaskID, Status: TaskStatusRunning, StartedAt: &now})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "deploying", "", "")
	logger := deployLogger{server: s, task: task}
	logger.info("start", "update task started")

	result, err := s.updateNode(ctx, task, logger)
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
	logger.info("done", "update task completed")
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{
		TaskID:     task.TaskID,
		Status:     TaskStatusSuccess,
		ResultJSON: result,
		FinishedAt: &finishedAt,
	})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "online", result.NodeVersion, "")
}

func (s *Server) updateNode(ctx context.Context, task NodeTask, logger deployLogger) (UpdateNodeTaskResult, error) {
	req, err := updateRequestFromTask(task)
	if err != nil {
		return UpdateNodeTaskResult{}, err
	}
	node, err := s.store.GetNode(ctx, task.NodeID)
	if err != nil {
		return UpdateNodeTaskResult{}, err
	}
	credential, err := s.store.GetNodeCredential(ctx, task.NodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return UpdateNodeTaskResult{}, errors.New("node SSH credential is not configured")
		}
		return UpdateNodeTaskResult{}, err
	}
	client, err := s.openSSHClient(ctx, *node, *credential)
	if err != nil {
		return UpdateNodeTaskResult{}, err
	}
	defer client.Close()
	_ = s.store.MarkNodeCredentialUsed(ctx, task.NodeID, time.Now().UTC())

	runner := sshRunner{
		client:  client,
		timeout: s.cfg.Deploy.CommandTimeout,
		root:    credential.SudoMode == CredentialSudoSudo,
	}
	adminURL := fmt.Sprintf("http://127.0.0.1:%d", node.AdminPort)
	result := UpdateNodeTaskResult{Version: req.Version, NodeID: node.NodeID, AdminURL: adminURL}

	logger.info("precheck", "checking target system")
	precheck := `command -v sh >/dev/null && command -v uname >/dev/null && command -v systemctl >/dev/null && command -v cp >/dev/null && command -v mv >/dev/null && command -v install >/dev/null && command -v sha256sum >/dev/null && command -v tar >/dev/null && command -v grep >/dev/null && command -v awk >/dev/null && (command -v curl >/dev/null || command -v wget >/dev/null)`
	if _, err := runner.run(ctx, "precheck", precheck, logger); err != nil {
		return result, fmt.Errorf("precheck failed: %w", err)
	}

	logger.info("release-check", "checking requested gaccel node release")
	if _, err := runner.runRoot(ctx, "release-check", nodePackageProbeCommand(req.Version), logger); err != nil {
		return result, fmt.Errorf("release check failed: %w", err)
	}

	logger.info("backup", "backing up current gaccel binaries")
	backupDir := "/var/lib/gaccel-node/backups/" + task.TaskID
	backupCmd := fmt.Sprintf(`set -eu
backup_dir=%s
install -d -m 0750 -o root -g root "$backup_dir"
for bin in /usr/local/bin/gaccel-node /usr/local/bin/gaccel-probe /usr/local/bin/gaccel-token /usr/local/bin/gaccel-token-api; do
  if [ -f "$bin" ]; then cp -p "$bin" "$backup_dir/"; fi
done
if [ -f /etc/systemd/system/gaccel-node.service ]; then cp -p /etc/systemd/system/gaccel-node.service "$backup_dir/gaccel-node.service"; fi
if [ -f /etc/gaccel-node/config.yaml ]; then cp -p /etc/gaccel-node/config.yaml "$backup_dir/config.yaml"; fi
printf '%%s' "$backup_dir"`, shellQuote(backupDir))
	backupOutput, err := runner.runRoot(ctx, "backup", backupCmd, logger)
	if err != nil {
		return result, fmt.Errorf("backup failed: %w", err)
	}
	result.BackupDir = strings.TrimSpace(backupOutput)
	if result.BackupDir == "" {
		result.BackupDir = backupDir
	}

	logger.info("install", "installing requested gaccel node version")
	if _, err := runner.runRoot(ctx, "install", nodePackageInstallCommand(req.Version), logger); err != nil {
		return rollbackUpdateFailure(ctx, runner, logger, result, fmt.Errorf("install failed: %w", err))
	}

	logger.info("systemd", "restarting gaccel-node")
	if _, err := runner.runRoot(ctx, "systemd", `systemctl daemon-reload
systemctl restart gaccel-node`, logger); err != nil {
		return rollbackUpdateFailure(ctx, runner, logger, result, fmt.Errorf("restart service failed: %w", err))
	}

	logger.info("verify", "checking updated node health and status")
	if _, err := runner.run(ctx, "verify-health", fmt.Sprintf("curl -fsS %s/health", shellQuote(adminURL)), logger); err != nil {
		return rollbackUpdateFailure(ctx, runner, logger, result, fmt.Errorf("health check failed: %w", err))
	}
	result.HealthOK = true
	if _, err := runner.run(ctx, "verify-status", fmt.Sprintf("curl -fsS %s/status", shellQuote(adminURL)), logger); err != nil {
		return rollbackUpdateFailure(ctx, runner, logger, result, fmt.Errorf("status check failed: %w", err))
	}
	result.StatusOK = true
	versionOutput, err := runner.run(ctx, "verify-version", "/usr/local/bin/gaccel-node -version", logger)
	if err != nil {
		return rollbackUpdateFailure(ctx, runner, logger, result, fmt.Errorf("version check failed: %w", err))
	}
	result.NodeVersion = strings.TrimSpace(versionOutput)
	if !updateVersionMatches(req.Version, result.NodeVersion) {
		return rollbackUpdateFailure(ctx, runner, logger, result, fmt.Errorf("updated version %q does not match requested %q", result.NodeVersion, req.Version))
	}
	return result, nil
}

func updateRequestFromTask(task NodeTask) (UpdateNodeTaskRequest, error) {
	var req UpdateNodeTaskRequest
	if len(task.RequestJSON) == 0 {
		return req, errors.New("update task request is empty")
	}
	if err := json.Unmarshal(task.RequestJSON, &req); err != nil {
		return req, fmt.Errorf("decode update request: %w", err)
	}
	req.Version = strings.TrimSpace(req.Version)
	if req.Version == "" {
		req.Version = "latest"
	}
	req.Version = normalizeNodeReleaseVersion(req.Version)
	return req, nil
}

func rollbackUpdateFailure(ctx context.Context, runner sshRunner, logger deployLogger, result UpdateNodeTaskResult, original error) (UpdateNodeTaskResult, error) {
	if strings.TrimSpace(result.BackupDir) == "" {
		return result, original
	}
	logger.info("rollback", "restoring previous gaccel binaries")
	rollbackCmd := fmt.Sprintf(`set -eu
backup_dir=%s
echo "rollback backup_dir=${backup_dir}"
if [ ! -d "$backup_dir" ]; then
  echo "backup directory does not exist: ${backup_dir}" >&2
  exit 10
fi
echo "--- backup directory ---"
ls -la "$backup_dir" || true
restore_binary() {
  name="$1"
  src="$backup_dir/$name"
  dest="/usr/local/bin/$name"
  tmp="${dest}.rollback.$$"
  if [ ! -f "$src" ]; then
    echo "skip missing backup binary: $name"
    return 0
  fi
  echo "restore binary: $name"
  rm -f "$tmp"
  cp -p "$src" "$tmp"
  chmod 0755 "$tmp"
  chown root:root "$tmp" 2>/dev/null || true
  mv -f "$tmp" "$dest"
}
for name in gaccel-node gaccel-probe gaccel-token gaccel-token-api; do
  restore_binary "$name"
done
if [ -f "$backup_dir/gaccel-node.service" ]; then
  echo "restore systemd service"
  cp -p "$backup_dir/gaccel-node.service" /etc/systemd/system/gaccel-node.service
else
  echo "skip missing backup service"
fi
if [ -f "$backup_dir/config.yaml" ]; then
  echo "restore config.yaml"
  cp -p "$backup_dir/config.yaml" /etc/gaccel-node/config.yaml
  chown root:gaccel /etc/gaccel-node/config.yaml 2>/dev/null || true
  chmod 0640 /etc/gaccel-node/config.yaml 2>/dev/null || true
else
  echo "skip missing backup config"
fi
systemctl daemon-reload
if ! systemctl restart gaccel-node; then
  echo "--- gaccel-node service status after rollback failure ---"
  systemctl status gaccel-node --no-pager -l || true
  echo "--- gaccel-node recent journal after rollback failure ---"
  journalctl -u gaccel-node -n 80 --no-pager || true
  exit 1
fi
echo "rollback restart ok"`, shellQuote(result.BackupDir))
	if _, err := runner.runRoot(ctx, "rollback", rollbackCmd, logger); err != nil {
		return result, fmt.Errorf("%w; rollback failed: %v (see rollback task logs)", original, err)
	}
	result.RolledBack = true
	return result, fmt.Errorf("%w; rolled back to %s", original, result.BackupDir)
}

func updateVersionMatches(requested string, actual string) bool {
	requested = strings.TrimSpace(strings.TrimPrefix(requested, "v"))
	actual = strings.TrimSpace(strings.TrimPrefix(actual, "v"))
	if requested == "" || requested == "latest" {
		return true
	}
	return actual == requested || strings.HasPrefix(actual, requested+"-")
}
