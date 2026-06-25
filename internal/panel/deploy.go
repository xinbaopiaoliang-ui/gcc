package panel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

const installScriptURL = "https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh"

type DeployNodeTaskRequest struct {
	Version      string `json:"version,omitempty"`
	HMACSecret   string `json:"hmac_secret,omitempty"`
	PanelBaseURL string `json:"panel_base_url,omitempty"`
	Priority     int    `json:"priority,omitempty"`
}

type storedDeployNodeTaskRequest struct {
	Version             string `json:"version"`
	HMACSecretEncrypted string `json:"hmac_secret_encrypted"`
	PanelBaseURL        string `json:"panel_base_url,omitempty"`
}

type DeployNodeTaskResult struct {
	Version     string `json:"version"`
	NodeID      string `json:"node_id"`
	AdminURL    string `json:"admin_url"`
	HealthOK    bool   `json:"health_ok"`
	StatusOK    bool   `json:"status_ok"`
	NodeVersion string `json:"node_version,omitempty"`
}

func NewDeployNodeTask(nodeID string, req DeployNodeTaskRequest, defaultVersion string, now time.Time, box *SecretBox) (NodeTaskInput, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodeTaskInput{}, errors.New("node_id is required")
	}
	if box == nil {
		return NodeTaskInput{}, errors.New("secret box is not configured")
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = strings.TrimSpace(defaultVersion)
	}
	if version == "" {
		version = "latest"
	}
	version = normalizeNodeReleaseVersion(version)
	hmacSecret := strings.TrimSpace(req.HMACSecret)
	var encryptedHMACSecret string
	if hmacSecret != "" {
		if len(hmacSecret) < 16 {
			return NodeTaskInput{}, errors.New("hmac_secret must be at least 16 characters")
		}
		var err error
		encryptedHMACSecret, err = box.Encrypt(hmacSecret)
		if err != nil {
			return NodeTaskInput{}, err
		}
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 50
	}
	taskID, err := newTaskID(TaskTypeDeployNode, now)
	if err != nil {
		return NodeTaskInput{}, err
	}
	return NodeTaskInput{
		TaskID:   taskID,
		NodeID:   nodeID,
		Type:     TaskTypeDeployNode,
		Status:   TaskStatusPending,
		Priority: priority,
		RequestJSON: storedDeployNodeTaskRequest{
			Version:             version,
			HMACSecretEncrypted: encryptedHMACSecret,
			PanelBaseURL:        strings.TrimSpace(req.PanelBaseURL),
		},
	}, nil
}

func (s *Server) runDeployNodeTask(task NodeTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{TaskID: task.TaskID, Status: TaskStatusRunning, StartedAt: &now})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "deploying", "", "")
	logger := deployLogger{server: s, task: task}
	logger.info("start", "deployment task started")

	result, err := s.deployNode(ctx, task, logger)
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
	logger.info("done", "deployment task completed")
	_, _ = s.store.UpdateNodeTask(ctx, NodeTaskUpdate{
		TaskID:     task.TaskID,
		Status:     TaskStatusSuccess,
		ResultJSON: result,
		FinishedAt: &finishedAt,
	})
	_ = s.store.UpdateNodeOperationalState(ctx, task.NodeID, "online", result.NodeVersion, "")
}

func (s *Server) deployNode(ctx context.Context, task NodeTask, logger deployLogger) (DeployNodeTaskResult, error) {
	req, err := s.deployRequestFromTask(task)
	if err != nil {
		return DeployNodeTaskResult{}, err
	}
	node, err := s.store.GetNode(ctx, task.NodeID)
	if err != nil {
		return DeployNodeTaskResult{}, err
	}
	hmacSecret, err := s.resolveDeployHMACSecret(*node, req)
	if err != nil {
		return DeployNodeTaskResult{}, err
	}
	credential, err := s.store.GetNodeCredential(ctx, task.NodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return DeployNodeTaskResult{}, errors.New("node SSH credential is not configured")
		}
		return DeployNodeTaskResult{}, err
	}
	client, err := s.openSSHClient(ctx, *node, *credential)
	if err != nil {
		return DeployNodeTaskResult{}, err
	}
	defer client.Close()
	_ = s.store.MarkNodeCredentialUsed(ctx, task.NodeID, time.Now().UTC())

	runner := sshRunner{
		client:  client,
		timeout: s.cfg.Deploy.CommandTimeout,
		root:    credential.SudoMode == CredentialSudoSudo,
	}
	logger.info("precheck", "checking target system")
	if _, err := runner.run(ctx, "precheck", `command -v sh >/dev/null && command -v uname >/dev/null && command -v systemctl >/dev/null && command -v tar >/dev/null && command -v sha256sum >/dev/null && command -v grep >/dev/null && command -v awk >/dev/null && (command -v curl >/dev/null || command -v wget >/dev/null) && command -v base64 >/dev/null && command -v openssl >/dev/null`, logger); err != nil {
		return DeployNodeTaskResult{}, fmt.Errorf("precheck failed: %w", err)
	}

	logger.info("release-check", "checking requested gaccel node release")
	if _, err := runner.runRoot(ctx, "release-check", nodePackageProbeCommand(req.Version), logger); err != nil {
		return DeployNodeTaskResult{}, fmt.Errorf("release check failed: %w", err)
	}

	logger.info("install", "installing gaccel node package")
	if _, err := runner.runRoot(ctx, "install", nodePackageInstallCommand(req.Version), logger); err != nil {
		return DeployNodeTaskResult{}, fmt.Errorf("install failed: %w", err)
	}

	panelBaseURL := strings.TrimRight(req.PanelBaseURL, "/")
	if panelBaseURL == "" {
		panelBaseURL = strings.TrimRight(s.cfg.PublicBaseURL, "/")
	}
	configYAML, err := s.renderNodeConfig(*node, hmacSecret, panelBaseURL)
	if err != nil {
		return DeployNodeTaskResult{}, err
	}
	logger.info("config", "writing node config")
	configB64 := base64.StdEncoding.EncodeToString([]byte(configYAML))
	writeConfigCmd := fmt.Sprintf(`install -d -m 0750 -o root -g gaccel /etc/gaccel-node
base64 -d > /etc/gaccel-node/config.yaml <<'GACCEL_CONFIG_B64'
%s
GACCEL_CONFIG_B64
chown root:gaccel /etc/gaccel-node/config.yaml
chmod 0640 /etc/gaccel-node/config.yaml`, configB64)
	if _, err := runner.runRoot(ctx, "config", writeConfigCmd, logger); err != nil {
		return DeployNodeTaskResult{}, fmt.Errorf("write config failed: %w", err)
	}

	logger.info("cert", "ensuring self-signed TLS certificate")
	certCmd := fmt.Sprintf(`install -d -m 0750 -o root -g gaccel /etc/gaccel-node
if [ ! -f /etc/gaccel-node/cert.pem ] || [ ! -f /etc/gaccel-node/key.pem ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -keyout /etc/gaccel-node/key.pem -out /etc/gaccel-node/cert.pem -days 3650 -subj %s >/dev/null 2>&1
fi
chown root:gaccel /etc/gaccel-node/key.pem /etc/gaccel-node/cert.pem
chmod 0640 /etc/gaccel-node/key.pem
chmod 0644 /etc/gaccel-node/cert.pem`, shellQuote("/CN="+node.NodeID))
	if _, err := runner.runRoot(ctx, "cert", certCmd, logger); err != nil {
		return DeployNodeTaskResult{}, fmt.Errorf("ensure cert failed: %w", err)
	}

	logger.info("systemd", "starting gaccel-node")
	if _, err := runner.runRoot(ctx, "systemd", `systemctl daemon-reload
systemctl enable gaccel-node
systemctl restart gaccel-node`, logger); err != nil {
		return DeployNodeTaskResult{}, fmt.Errorf("start service failed: %w", err)
	}

	adminURL := fmt.Sprintf("http://127.0.0.1:%d", node.AdminPort)
	result := DeployNodeTaskResult{Version: req.Version, NodeID: node.NodeID, AdminURL: adminURL}
	logger.info("verify", "checking node health and status")
	if _, err := runner.run(ctx, "verify-health", fmt.Sprintf("curl -fsS %s/health", shellQuote(adminURL)), logger); err != nil {
		return result, fmt.Errorf("health check failed: %w", err)
	}
	result.HealthOK = true
	if _, err := runner.run(ctx, "verify-status", fmt.Sprintf("curl -fsS %s/status", shellQuote(adminURL)), logger); err != nil {
		return result, fmt.Errorf("status check failed: %w", err)
	}
	result.StatusOK = true
	versionOutput, err := runner.run(ctx, "verify-version", "/usr/local/bin/gaccel-node -version", logger)
	if err == nil {
		result.NodeVersion = strings.TrimSpace(versionOutput)
	}
	return result, nil
}

func (s *Server) deployRequestFromTask(task NodeTask) (DeployNodeTaskRequest, error) {
	var stored storedDeployNodeTaskRequest
	if len(task.RequestJSON) == 0 {
		return DeployNodeTaskRequest{}, errors.New("deploy task request is empty")
	}
	if err := json.Unmarshal(task.RequestJSON, &stored); err != nil {
		return DeployNodeTaskRequest{}, fmt.Errorf("decode deploy request: %w", err)
	}
	var hmacSecret string
	if strings.TrimSpace(stored.HMACSecretEncrypted) != "" {
		var err error
		hmacSecret, err = s.secrets.Decrypt(stored.HMACSecretEncrypted)
		if err != nil {
			return DeployNodeTaskRequest{}, err
		}
	}
	req := DeployNodeTaskRequest{
		Version:      strings.TrimSpace(stored.Version),
		HMACSecret:   strings.TrimSpace(hmacSecret),
		PanelBaseURL: strings.TrimSpace(stored.PanelBaseURL),
	}
	req.Version = strings.TrimSpace(req.Version)
	req.HMACSecret = strings.TrimSpace(req.HMACSecret)
	req.PanelBaseURL = strings.TrimSpace(req.PanelBaseURL)
	if req.Version == "" {
		req.Version = "latest"
	}
	req.Version = normalizeNodeReleaseVersion(req.Version)
	if req.HMACSecret != "" && len(req.HMACSecret) < 16 {
		return req, errors.New("hmac_secret must be at least 16 characters")
	}
	return req, nil
}

func (s *Server) resolveDeployHMACSecret(node Node, req DeployNodeTaskRequest) (string, error) {
	if s.secrets == nil {
		return "", errors.New("secret box is not configured")
	}
	if hmacSecret := strings.TrimSpace(req.HMACSecret); hmacSecret != "" {
		if len(hmacSecret) < 16 {
			return "", errors.New("hmac_secret must be at least 16 characters")
		}
		return hmacSecret, nil
	}
	if !node.HMACSecretConfigured || strings.TrimSpace(node.HMACSecretEncrypted) == "" {
		return "", errors.New("node hmac_secret is not configured; sync it from backend before deploy")
	}
	hmacSecret, err := s.secrets.Decrypt(node.HMACSecretEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt node hmac_secret: %w", err)
	}
	hmacSecret = strings.TrimSpace(hmacSecret)
	if len(hmacSecret) < 16 {
		return "", errors.New("stored node hmac_secret is invalid; sync it from backend again")
	}
	return hmacSecret, nil
}

func (s *Server) renderNodeConfig(node Node, hmacSecret string, panelBaseURL string) (string, error) {
	if len(s.cfg.Security.BackendAPIKeys) == 0 {
		return "", errors.New("panel backend API key is not configured")
	}
	panel := map[string]any{
		"report_url":             "",
		"command_url":            "",
		"api_key":                "",
		"command_secret":         "",
		"interval":               "30s",
		"timeout":                "10s",
		"command_interval":       "30s",
		"command_timeout":        "10s",
		"command_max_clock_skew": "2m",
	}
	if panelBaseURL != "" {
		if _, err := url.ParseRequestURI(panelBaseURL); err != nil {
			return "", fmt.Errorf("panel_base_url is invalid: %w", err)
		}
		panel["report_url"] = strings.TrimRight(panelBaseURL, "/") + "/api/nodes/report"
		panel["command_url"] = strings.TrimRight(panelBaseURL, "/") + "/api/nodes/commands"
		panel["api_key"] = s.cfg.Security.BackendAPIKeys[0]
		panel["command_secret"] = s.cfg.NodeCommand.Secret
	}
	payload := map[string]any{
		"server": map[string]any{
			"listen":    fmt.Sprintf(":%d", node.EndpointPort),
			"alpn":      node.ALPN,
			"cert_file": "/etc/gaccel-node/cert.pem",
			"key_file":  "/etc/gaccel-node/key.pem",
		},
		"node": map[string]any{
			"id":     node.NodeID,
			"region": node.Region,
			"tags":   node.Tags,
			"labels": node.Labels,
		},
		"auth": map[string]any{
			"mode":         "hmac",
			"hmac_secret":  hmacSecret,
			"token_leeway": "30s",
		},
		"limits": map[string]any{
			"max_quic_connections": 50000,
			"max_user_connections": 8,
			"max_flows_per_conn":   256,
			"quic_idle_timeout":    "60s",
			"udp_idle_timeout":     "60s",
			"tcp_idle_timeout":     "10m",
			"user_rate_limit_mbps": 100,
		},
		"security": map[string]any{
			"deny_private_ip":     true,
			"deny_loopback":       true,
			"deny_link_local":     true,
			"deny_multicast":      true,
			"deny_cloud_metadata": true,
			"allowed_udp_ports":   []string{"1-65535"},
			"allowed_tcp_ports":   []string{"80", "443", "1935", "5222", "27000-65535"},
			"blocked_tcp_ports":   []string{"22", "25", "3306", "5432", "6379"},
		},
		"panel": panel,
		"upgrade": map[string]any{
			"stage_dir":         "/var/lib/gaccel-node/upgrades",
			"max_package_bytes": 209715200,
			"timeout":           "2m",
			"allow_http":        false,
		},
		"admin": map[string]any{
			"listen": nodeAdminListenAddress(node),
		},
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func nodeAdminListenAddress(node Node) string {
	host := "127.0.0.1"
	if strings.TrimSpace(node.AdminHost) != "" && !isLoopbackHost(node.AdminHost) {
		host = "0.0.0.0"
	}
	port := node.AdminPort
	if port <= 0 {
		port = 5557
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func normalizeNodeReleaseVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "latest"
	}
	if strings.EqualFold(version, "latest") {
		return "latest"
	}
	if strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		return "v" + strings.TrimSpace(version[1:])
	}
	if version[0] >= '0' && version[0] <= '9' {
		return "v" + version
	}
	return version
}

func nodePackageProbeCommand(version string) string {
	return fmt.Sprintf(`set -eu
repo="${REPO:-xinbaopiaoliang-ui/gcc}"
version=%s
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() {
  url="$1"
  dest="$2"
  label="$3"
  echo "download ${label}: ${url}"
  if command -v curl >/dev/null 2>&1; then
    if ! curl --fail --location --show-error --silent "$url" -o "$dest"; then
      echo "download failed: ${label}" >&2
      return 22
    fi
  else
    if ! wget -S -O "$dest" "$url"; then
      echo "download failed: ${label}" >&2
      return 22
    fi
  fi
}

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  base_url="https://github.com/${repo}/releases/latest/download"
else
  base_url="https://github.com/${repo}/releases/download/${version}"
fi
echo "requested node version: ${version}"
echo "release base: ${base_url}"

checksums="$tmp/SHA256SUMS"
fetch "${base_url}/SHA256SUMS" "$checksums" "SHA256SUMS"
archive_name="$(grep "linux-${arch}.tar.gz" "$checksums" | awk '{print $2}' | head -n 1)"
if [ -z "$archive_name" ]; then
  echo "cannot find linux-${arch} archive in SHA256SUMS" >&2
  exit 1
fi
echo "release archive: ${archive_name}"
fetch "${base_url}/${archive_name}" "$tmp/${archive_name}.probe" "release archive probe"`, shellQuote(normalizeNodeReleaseVersion(version)))
}

func nodePackageInstallCommand(version string) string {
	return fmt.Sprintf(`set -eu
repo="${REPO:-xinbaopiaoliang-ui/gcc}"
version=%s
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() {
  url="$1"
  dest="$2"
  label="$3"
  echo "download ${label}: ${url}"
  if command -v curl >/dev/null 2>&1; then
    if ! curl --fail --location --show-error --silent "$url" -o "$dest"; then
      echo "download failed: ${label}" >&2
      return 22
    fi
  else
    if ! wget -S -O "$dest" "$url"; then
      echo "download failed: ${label}" >&2
      return 22
    fi
  fi
}

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  base_url="https://github.com/${repo}/releases/latest/download"
else
  base_url="https://github.com/${repo}/releases/download/${version}"
fi
echo "requested node version: ${version}"
echo "release base: ${base_url}"

checksums="$tmp/SHA256SUMS"
fetch "${base_url}/SHA256SUMS" "$checksums" "SHA256SUMS"
archive_name="$(grep "linux-${arch}.tar.gz" "$checksums" | awk '{print $2}' | head -n 1)"
if [ -z "$archive_name" ]; then
  echo "cannot find linux-${arch} archive in SHA256SUMS" >&2
  exit 1
fi
echo "release archive: ${archive_name}"
fetch "${base_url}/${archive_name}" "$tmp/${archive_name}.probe" "release archive probe"

fetch %s "$tmp/install.sh" "install script"
VERSION="$version" REPO="$repo" sh "$tmp/install.sh"`, shellQuote(normalizeNodeReleaseVersion(version)), shellQuote(installScriptURL))
}

func (s *Server) openSSHClient(ctx context.Context, node Node, credential NodeCredential) (*ssh.Client, error) {
	address := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
	authMethods, err := s.sshAuthMethods(credential)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            credential.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         s.cfg.Deploy.SSHTimeout,
	}
	dialer := net.Dialer{Timeout: s.cfg.Deploy.SSHTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

type sshRunner struct {
	client  *ssh.Client
	timeout time.Duration
	root    bool
}

func (r sshRunner) run(ctx context.Context, step string, command string, logger deployLogger) (string, error) {
	return r.runCommand(ctx, step, command, logger)
}

func (r sshRunner) runRoot(ctx context.Context, step string, command string, logger deployLogger) (string, error) {
	if r.root {
		command = "sudo sh -c " + shellQuote(command)
	} else {
		command = "sh -c " + shellQuote(command)
	}
	return r.runCommand(ctx, step, command, logger)
}

func (r sshRunner) runCommand(ctx context.Context, step string, command string, logger deployLogger) (string, error) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var output bytes.Buffer
	session.Stdout = &output
	session.Stderr = &output
	errCh := make(chan error, 1)
	go func() { errCh <- session.Run(command) }()
	select {
	case <-runCtx.Done():
		_ = session.Signal(ssh.SIGKILL)
		logger.error(step, "command timed out")
		return output.String(), runCtx.Err()
	case err := <-errCh:
		text := strings.TrimSpace(output.String())
		if text != "" {
			logger.stdout(step, text)
		}
		return output.String(), err
	}
}

type deployLogger struct {
	server *Server
	task   NodeTask
}

func (l deployLogger) info(step string, message string) {
	l.append(step, "info", message)
}

func (l deployLogger) stdout(step string, message string) {
	l.append(step, "stdout", message)
}

func (l deployLogger) error(step string, message string) {
	l.append(step, "error", message)
}

func (l deployLogger) append(step string, stream string, message string) {
	if l.server == nil || l.server.store == nil {
		return
	}
	_, _ = l.server.store.AppendNodeTaskLog(context.Background(), NodeTaskLogInput{
		TaskID:  l.task.TaskID,
		NodeID:  l.task.NodeID,
		Step:    step,
		Stream:  stream,
		Message: sanitizeDeployLog(message),
	})
}

func sanitizeDeployLog(message string) string {
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "hmac_secret") || strings.Contains(lower, "api_key") || strings.Contains(lower, "command_secret") {
			lines[i] = "[redacted secret line]"
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
