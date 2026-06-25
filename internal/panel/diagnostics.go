package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DiagnosticStatusOK      = "ok"
	DiagnosticStatusWarning = "warning"
	DiagnosticStatusError   = "error"
)

var requiredPanelTables = []string{
	"panel_users",
	"panel_nodes",
	"panel_node_credentials",
	"panel_node_reports",
	"panel_node_tasks",
	"panel_node_task_logs",
	"panel_policy_revisions",
	"panel_node_policy_revisions",
	"panel_token_defaults",
	"panel_audit_logs",
}

type DiagnosticCheck struct {
	Key     string         `json:"key"`
	Label   string         `json:"label"`
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type DiagnosticSummary struct {
	OK      int `json:"ok"`
	Warning int `json:"warning"`
	Error   int `json:"error"`
	Total   int `json:"total"`
}

type SystemConfigSnapshot struct {
	Listen                string   `json:"listen"`
	PublicBaseURL         string   `json:"public_base_url,omitempty"`
	DatabaseDriver        string   `json:"database_driver"`
	WebRoot               string   `json:"web_root,omitempty"`
	BackendAPIKeyCount    int      `json:"backend_api_key_count"`
	SessionTTLSeconds     int64    `json:"session_ttl_seconds"`
	CORSAllowedOrigins    []string `json:"cors_allowed_origins"`
	DefaultNodeVersion    string   `json:"default_node_version"`
	SSHTimeoutSeconds     int64    `json:"ssh_timeout_seconds"`
	CommandTimeoutSeconds int64    `json:"command_timeout_seconds"`
}

type SystemCheckResponse struct {
	Status      string               `json:"status"`
	Version     string               `json:"version"`
	GeneratedAt time.Time            `json:"generated_at"`
	Config      SystemConfigSnapshot `json:"config"`
	Summary     DiagnosticSummary    `json:"summary"`
	Checks      []DiagnosticCheck    `json:"checks"`
}

type SchemaTableCheck struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

type NodeDiagnosticsResponse struct {
	Status          string            `json:"status"`
	NodeID          string            `json:"node_id"`
	GeneratedAt     time.Time         `json:"generated_at"`
	AdminURL        string            `json:"admin_url"`
	SyncStatus      NodeSyncStatus    `json:"sync_status"`
	Summary         DiagnosticSummary `json:"summary"`
	Checks          []DiagnosticCheck `json:"checks"`
	Recommendations []string          `json:"recommendations"`
}

type databaseHealthChecker interface {
	Ping(ctx context.Context) error
	CheckRequiredTables(ctx context.Context, tables []string) ([]SchemaTableCheck, error)
}

func (s *Server) handlePanelSystemCheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanelAdmin(w, r); !ok {
		return
	}
	s.handleSystemCheck(w, r)
}

func (s *Server) handleBackendSystemCheck(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	s.handleSystemCheck(w, r)
}

func (s *Server) handleSystemCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	resp := s.BuildSystemCheck(r.Context(), time.Now().UTC())
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) BuildSystemCheck(ctx context.Context, now time.Time) SystemCheckResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cfg := s.cfg
	resp := SystemCheckResponse{
		Version:     s.version,
		GeneratedAt: now,
		Checks:      make([]DiagnosticCheck, 0),
	}
	if cfg == nil {
		resp.Checks = append(resp.Checks, newDiagnosticCheck("config.present", "面板配置", DiagnosticStatusError, "面板配置未加载", nil))
		resp.Summary = summarizeDiagnosticChecks(resp.Checks)
		resp.Status = overallDiagnosticStatus(resp.Summary)
		return resp
	}

	resp.Config = SystemConfigSnapshot{
		Listen:                cfg.Listen,
		PublicBaseURL:         cfg.PublicBaseURL,
		DatabaseDriver:        cfg.Database.Driver,
		WebRoot:               cfg.Web.Root,
		BackendAPIKeyCount:    len(cfg.Security.BackendAPIKeys),
		SessionTTLSeconds:     int64(cfg.Session.TTL.Seconds()),
		CORSAllowedOrigins:    append([]string{}, cfg.CORS.AllowedOrigins...),
		DefaultNodeVersion:    cfg.Deploy.DefaultNodeVersion,
		SSHTimeoutSeconds:     int64(cfg.Deploy.SSHTimeout.Seconds()),
		CommandTimeoutSeconds: int64(cfg.Deploy.CommandTimeout.Seconds()),
	}

	resp.Checks = append(resp.Checks, s.checkListenConfig(cfg)...)
	resp.Checks = append(resp.Checks, s.checkSecurityConfig(cfg)...)
	resp.Checks = append(resp.Checks, s.checkWebRoot(cfg)...)
	resp.Checks = append(resp.Checks, s.checkDatabase(ctx)...)
	resp.Checks = append(resp.Checks, s.checkStoreContent(ctx)...)

	resp.Summary = summarizeDiagnosticChecks(resp.Checks)
	resp.Status = overallDiagnosticStatus(resp.Summary)
	return resp
}

func (s *Server) checkListenConfig(cfg *Config) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 4)
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		checks = append(checks, newDiagnosticCheck("listen.address", "监听地址", DiagnosticStatusError, "listen 格式无效，应类似 127.0.0.1:18091", map[string]any{"listen": cfg.Listen}))
	} else {
		status := DiagnosticStatusOK
		message := "面板监听地址格式正确"
		if host == "" || host == "0.0.0.0" || host == "::" {
			status = DiagnosticStatusWarning
			message = "面板直接监听全部网卡，建议通过 Nginx 反代并限制访问来源"
		}
		checks = append(checks, newDiagnosticCheck("listen.address", "监听地址", status, message, map[string]any{"host": host, "port": port}))
	}
	if strings.TrimSpace(cfg.PublicBaseURL) == "" {
		checks = append(checks, newDiagnosticCheck("public_base_url", "公网入口", DiagnosticStatusWarning, "public_base_url 未配置，一键部署和业务后台回调时需要人工确认面板地址", nil))
	} else if _, err := url.ParseRequestURI(cfg.PublicBaseURL); err != nil {
		checks = append(checks, newDiagnosticCheck("public_base_url", "公网入口", DiagnosticStatusWarning, "public_base_url 不是标准 URL，请检查业务后台填写值", map[string]any{"public_base_url": cfg.PublicBaseURL}))
	} else {
		checks = append(checks, newDiagnosticCheck("public_base_url", "公网入口", DiagnosticStatusOK, "public_base_url 已配置", map[string]any{"public_base_url": cfg.PublicBaseURL}))
	}
	if len(cfg.CORS.AllowedOrigins) == 0 {
		checks = append(checks, newDiagnosticCheck("cors.origins", "跨域来源", DiagnosticStatusWarning, "未配置 CORS 来源，前后端分离部署会被浏览器拦截", nil))
	} else {
		status := DiagnosticStatusOK
		message := "CORS 来源已配置"
		for _, origin := range cfg.CORS.AllowedOrigins {
			if origin == "*" {
				status = DiagnosticStatusWarning
				message = "CORS 允许全部来源，生产环境建议只填写前端站点地址"
				break
			}
		}
		checks = append(checks, newDiagnosticCheck("cors.origins", "跨域来源", status, message, map[string]any{"allowed_origins": cfg.CORS.AllowedOrigins}))
	}
	return checks
}

func (s *Server) checkSecurityConfig(cfg *Config) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 5)
	checks = append(checks, configuredCheck("security.master_key", "主密钥", strings.TrimSpace(cfg.Security.MasterKey) != "", "master_key 已配置，用于加密 SSH 凭据", "master_key 未配置，SSH 凭据无法安全保存"))
	checks = append(checks, configuredCheck("session.secret", "面板 JWT", strings.TrimSpace(cfg.Session.Secret) != "", "session.secret 已配置，Bearer JWT 可签发", "session.secret 未配置，面板登录不可用"))
	checks = append(checks, configuredCheck("node_command.secret", "节点命令签名", strings.TrimSpace(cfg.NodeCommand.Secret) != "", "node_command.secret 已配置，可签名节点命令", "node_command.secret 未配置，节点命令无法可信下发"))
	if len(cfg.Security.BackendAPIKeys) == 0 {
		checks = append(checks, newDiagnosticCheck("backend_api_keys", "业务后台 API Key", DiagnosticStatusError, "未配置业务后台 API Key，业务后台不能同步节点和策略", nil))
	} else {
		checks = append(checks, newDiagnosticCheck("backend_api_keys", "业务后台 API Key", DiagnosticStatusOK, "业务后台 API Key 已配置", map[string]any{"count": len(cfg.Security.BackendAPIKeys)}))
	}
	if cfg.Session.TTL <= 0 {
		checks = append(checks, newDiagnosticCheck("session.ttl", "登录有效期", DiagnosticStatusError, "session.ttl 必须大于 0", nil))
	} else {
		status := DiagnosticStatusOK
		message := "登录有效期正常"
		if cfg.Session.TTL > 24*time.Hour {
			status = DiagnosticStatusWarning
			message = "登录有效期较长，建议生产环境不超过 24 小时"
		}
		checks = append(checks, newDiagnosticCheck("session.ttl", "登录有效期", status, message, map[string]any{"ttl_seconds": int64(cfg.Session.TTL.Seconds())}))
	}
	return checks
}

func (s *Server) checkWebRoot(cfg *Config) []DiagnosticCheck {
	if strings.TrimSpace(cfg.Web.Root) == "" {
		return []DiagnosticCheck{
			newDiagnosticCheck("web.root", "静态目录", DiagnosticStatusWarning, "web.root 未配置，Go 后端不会托管前端静态文件；前后端分离部署时可忽略", nil),
		}
	}
	info, err := os.Stat(cfg.Web.Root)
	if err != nil {
		return []DiagnosticCheck{
			newDiagnosticCheck("web.root", "静态目录", DiagnosticStatusWarning, "web.root 无法访问；如果前端放在 PHP 项目，可忽略该项", map[string]any{"web_root": cfg.Web.Root, "error": err.Error()}),
		}
	}
	if !info.IsDir() {
		return []DiagnosticCheck{
			newDiagnosticCheck("web.root", "静态目录", DiagnosticStatusWarning, "web.root 不是目录", map[string]any{"web_root": cfg.Web.Root}),
		}
	}
	return []DiagnosticCheck{
		newDiagnosticCheck("web.root", "静态目录", DiagnosticStatusOK, "静态目录可访问", map[string]any{"web_root": cfg.Web.Root}),
	}
}

func (s *Server) checkDatabase(ctx context.Context) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 2)
	if s.store == nil {
		return []DiagnosticCheck{
			newDiagnosticCheck("database.store", "数据库连接", DiagnosticStatusError, "数据库 store 未配置", nil),
		}
	}
	checker, ok := s.store.(databaseHealthChecker)
	if !ok {
		return []DiagnosticCheck{
			newDiagnosticCheck("database.store", "数据库连接", DiagnosticStatusWarning, "当前 store 不支持数据库自检；内存测试环境可忽略", nil),
		}
	}
	if err := checker.Ping(ctx); err != nil {
		checks = append(checks, newDiagnosticCheck("database.ping", "数据库连接", DiagnosticStatusError, "数据库 Ping 失败", map[string]any{"error": err.Error()}))
	} else {
		checks = append(checks, newDiagnosticCheck("database.ping", "数据库连接", DiagnosticStatusOK, "数据库连接正常", nil))
	}
	tableChecks, err := checker.CheckRequiredTables(ctx, requiredPanelTables)
	if err != nil {
		checks = append(checks, newDiagnosticCheck("database.schema", "数据库表结构", DiagnosticStatusError, "检查数据库表结构失败", map[string]any{"error": err.Error()}))
		return checks
	}
	missing := make([]string, 0)
	for _, item := range tableChecks {
		if !item.Exists {
			missing = append(missing, item.Name)
		}
	}
	if len(missing) > 0 {
		checks = append(checks, newDiagnosticCheck("database.schema", "数据库表结构", DiagnosticStatusError, "缺少必要数据库表，请重新导入 migrations/panel_schema.sql", map[string]any{"missing_tables": missing, "tables": tableChecks}))
	} else {
		checks = append(checks, newDiagnosticCheck("database.schema", "数据库表结构", DiagnosticStatusOK, "必要数据库表已就绪", map[string]any{"tables": tableChecks}))
	}
	return checks
}

func (s *Server) checkStoreContent(ctx context.Context) []DiagnosticCheck {
	if s.store == nil {
		return nil
	}
	checks := make([]DiagnosticCheck, 0, 2)
	users, err := s.store.ListPanelUsers(ctx)
	if err != nil {
		checks = append(checks, newDiagnosticCheck("panel_users", "面板账号", DiagnosticStatusError, "读取面板账号失败", map[string]any{"error": err.Error()}))
	} else if len(users) == 0 {
		checks = append(checks, newDiagnosticCheck("panel_users", "面板账号", DiagnosticStatusError, "没有任何面板账号，请先创建管理员账号", nil))
	} else {
		admins := 0
		active := 0
		for _, user := range users {
			if user.Role == PanelUserRoleAdmin && user.Status == PanelUserStatusActive {
				admins++
			}
			if user.Status == PanelUserStatusActive {
				active++
			}
		}
		status := DiagnosticStatusOK
		message := "面板账号可用"
		if admins == 0 {
			status = DiagnosticStatusError
			message = "没有启用状态的管理员账号"
		}
		checks = append(checks, newDiagnosticCheck("panel_users", "面板账号", status, message, map[string]any{"total": len(users), "active": active, "active_admins": admins}))
	}
	nodes, err := s.store.ListNodes(ctx, NodeListFilter{Limit: 10000})
	if err != nil {
		checks = append(checks, newDiagnosticCheck("panel_nodes", "节点数据", DiagnosticStatusError, "读取节点列表失败", map[string]any{"error": err.Error()}))
	} else {
		status := DiagnosticStatusOK
		message := "节点列表读取正常"
		if len(nodes) == 0 {
			status = DiagnosticStatusWarning
			message = "当前还没有节点，业务后台同步或手动新增后才会显示"
		}
		checks = append(checks, newDiagnosticCheck("panel_nodes", "节点数据", status, message, map[string]any{"count": len(nodes)}))
	}
	return checks
}

func (s *Server) handleGetNodeDiagnostics(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	resp, err := s.BuildNodeDiagnostics(r.Context(), strings.TrimSpace(nodeID), time.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_node_id", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) BuildNodeDiagnostics(ctx context.Context, nodeID string, now time.Time) (NodeDiagnosticsResponse, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !nodeIDPattern.MatchString(nodeID) {
		return NodeDiagnosticsResponse{}, fmt.Errorf("node_id is invalid")
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return NodeDiagnosticsResponse{}, err
	}
	reports, err := s.store.ListNodeReports(ctx, nodeID, 5)
	if err != nil {
		return NodeDiagnosticsResponse{}, err
	}
	tasks, err := s.store.ListNodeTasks(ctx, nodeID, 20)
	if err != nil {
		return NodeDiagnosticsResponse{}, err
	}

	adminURL := nodeAdminBaseURL(*node)
	resp := NodeDiagnosticsResponse{
		NodeID:          nodeID,
		GeneratedAt:     now,
		AdminURL:        adminURL,
		SyncStatus:      BuildNodeSyncStatus(*node, reports, tasks, now),
		Checks:          make([]DiagnosticCheck, 0),
		Recommendations: make([]string, 0),
	}
	resp.Checks = append(resp.Checks, nodeConfigChecks(*node)...)
	resp.Checks = append(resp.Checks, nodeSyncChecks(resp.SyncStatus)...)
	resp.Checks = append(resp.Checks, s.nodeCredentialCheck(ctx, nodeID))
	resp.Checks = append(resp.Checks, probeNodeAdmin(ctx, *node, adminURL)...)
	resp.Recommendations = nodeRecommendations(resp.Checks, resp.SyncStatus)
	resp.Summary = summarizeDiagnosticChecks(resp.Checks)
	resp.Status = overallDiagnosticStatus(resp.Summary)
	return resp, nil
}

func nodeAdminBaseURL(node Node) string {
	host := strings.TrimSpace(node.AdminHost)
	if host == "" {
		host = strings.TrimSpace(node.EndpointHost)
	}
	port := node.AdminPort
	if port <= 0 {
		port = 5557
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func nodeConfigChecks(node Node) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 4)
	if strings.TrimSpace(node.EndpointHost) == "" || node.EndpointPort <= 0 {
		checks = append(checks, newDiagnosticCheck("node.endpoint", "客户端入口", DiagnosticStatusError, "节点客户端入口 host/port 不完整", map[string]any{"endpoint_host": node.EndpointHost, "endpoint_port": node.EndpointPort}))
	} else {
		checks = append(checks, newDiagnosticCheck("node.endpoint", "客户端入口", DiagnosticStatusOK, "客户端 QUIC 入口已配置", map[string]any{"endpoint": net.JoinHostPort(node.EndpointHost, strconv.Itoa(node.EndpointPort)), "alpn": node.ALPN}))
	}
	if strings.TrimSpace(node.AdminHost) == "" || node.AdminPort <= 0 {
		checks = append(checks, newDiagnosticCheck("node.admin", "节点 Admin 入口", DiagnosticStatusError, "节点 admin_host/admin_port 不完整，面板无法探测节点状态", map[string]any{"admin_host": node.AdminHost, "admin_port": node.AdminPort}))
	} else {
		status := DiagnosticStatusOK
		message := "节点 Admin 入口已配置"
		if isLoopbackHost(node.AdminHost) && !isLoopbackHost(node.EndpointHost) {
			status = DiagnosticStatusWarning
			message = "admin_host 是本机回环地址；只有面板和节点在同一台服务器或配置了反代时才能探测"
		}
		checks = append(checks, newDiagnosticCheck("node.admin", "节点 Admin 入口", status, message, map[string]any{"admin": net.JoinHostPort(node.AdminHost, strconv.Itoa(node.AdminPort))}))
	}
	if !node.AllowTCP && !node.AllowUDP {
		checks = append(checks, newDiagnosticCheck("node.protocols", "协议能力", DiagnosticStatusError, "节点未启用 TCP 或 UDP，无法承担游戏加速转发", nil))
	} else {
		checks = append(checks, newDiagnosticCheck("node.protocols", "协议能力", DiagnosticStatusOK, "节点协议能力已配置", map[string]any{"allow_tcp": node.AllowTCP, "allow_udp": node.AllowUDP}))
	}
	if node.HMACSecretConfigured {
		checks = append(checks, newDiagnosticCheck("node.hmac_secret", "节点 HMAC Secret", DiagnosticStatusOK, "业务后台已同步节点 HMAC Secret", map[string]any{
			"source":     node.HMACSecretSource,
			"updated_at": node.HMACSecretUpdatedAt,
		}))
	} else {
		checks = append(checks, newDiagnosticCheck("node.hmac_secret", "节点 HMAC Secret", DiagnosticStatusWarning, "业务后台尚未同步节点 HMAC Secret，部署和客户端 token 签发不可用", nil))
	}
	if strings.TrimSpace(node.DesiredPolicyRevision) == "" {
		checks = append(checks, newDiagnosticCheck("node.desired_policy", "目标策略", DiagnosticStatusWarning, "节点未指定目标策略，强策略联调前需要从业务后台或面板下发 desired_policy_revision", nil))
	} else {
		checks = append(checks, newDiagnosticCheck("node.desired_policy", "目标策略", DiagnosticStatusOK, "目标策略已配置", map[string]any{"desired_policy_revision": node.DesiredPolicyRevision}))
	}
	return checks
}

func nodeSyncChecks(status NodeSyncStatus) []DiagnosticCheck {
	checks := make([]DiagnosticCheck, 0, 3)
	if status.LastReportAt == nil {
		checks = append(checks, newDiagnosticCheck("node.report", "节点上报", DiagnosticStatusWarning, "节点还没有上报过状态", nil))
	} else {
		level := DiagnosticStatusOK
		message := "节点最近上报正常"
		if status.ReportAgeSeconds != nil && *status.ReportAgeSeconds > 300 {
			level = DiagnosticStatusWarning
			message = "节点上报时间超过 5 分钟，请检查节点 report 配置"
		}
		checks = append(checks, newDiagnosticCheck("node.report", "节点上报", level, message, map[string]any{"last_report_at": status.LastReportAt, "report_age_seconds": status.ReportAgeSeconds}))
	}
	if status.PolicyState == "pending" || status.PolicyState == "waiting_report" {
		checks = append(checks, newDiagnosticCheck("node.policy_sync", "策略同步", DiagnosticStatusWarning, "节点策略未达到目标版本", map[string]any{"current_policy_revision": status.CurrentPolicyRevision, "desired_policy_revision": status.DesiredPolicyRevision, "state": status.PolicyState}))
	} else {
		checks = append(checks, newDiagnosticCheck("node.policy_sync", "策略同步", DiagnosticStatusOK, "策略同步状态正常", map[string]any{"state": status.PolicyState}))
	}
	if status.FailedTasks > 0 {
		checks = append(checks, newDiagnosticCheck("node.tasks", "任务队列", DiagnosticStatusWarning, "存在失败任务，请查看任务日志并按需重试", map[string]any{"failed_tasks": status.FailedTasks, "pending_tasks": status.PendingTasks, "running_tasks": status.RunningTasks}))
	} else {
		checks = append(checks, newDiagnosticCheck("node.tasks", "任务队列", DiagnosticStatusOK, "任务队列无失败任务", map[string]any{"pending_tasks": status.PendingTasks, "running_tasks": status.RunningTasks}))
	}
	return checks
}

func (s *Server) nodeCredentialCheck(ctx context.Context, nodeID string) DiagnosticCheck {
	if _, err := s.store.GetNodeCredential(ctx, nodeID); err == nil {
		return newDiagnosticCheck("node.credential", "SSH 凭据", DiagnosticStatusOK, "已保存 SSH 凭据，可执行一键部署/更新", nil)
	} else if errors.Is(err, ErrNotFound) {
		return newDiagnosticCheck("node.credential", "SSH 凭据", DiagnosticStatusWarning, "未保存 SSH 凭据；不影响客户端连接，但无法从面板执行部署/更新", nil)
	}
	return newDiagnosticCheck("node.credential", "SSH 凭据", DiagnosticStatusWarning, "读取 SSH 凭据状态失败", nil)
}

func probeNodeAdmin(ctx context.Context, node Node, adminURL string) []DiagnosticCheck {
	if strings.TrimSpace(node.AdminHost) == "" || node.AdminPort <= 0 {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 4 * time.Second}
	checks := make([]DiagnosticCheck, 0, 3)
	healthStatus, healthErr := probeAdminJSON(probeCtx, client, adminURL+"/health")
	if healthErr != nil {
		checks = append(checks, newDiagnosticCheck("admin.health", "Admin /health", DiagnosticStatusWarning, "无法访问节点 /health", map[string]any{"url": adminURL + "/health", "error": healthErr.Error()}))
	} else {
		checks = append(checks, newDiagnosticCheck("admin.health", "Admin /health", DiagnosticStatusOK, "节点 /health 可访问", map[string]any{"status_code": healthStatus.StatusCode, "body": healthStatus.Body}))
	}
	statusResp, statusErr := probeAdminJSON(probeCtx, client, adminURL+"/status")
	if statusErr != nil {
		checks = append(checks, newDiagnosticCheck("admin.status", "Admin /status", DiagnosticStatusWarning, "无法访问节点 /status", map[string]any{"url": adminURL + "/status", "error": statusErr.Error()}))
	} else {
		checks = append(checks, nodeStatusProbeCheck(node, statusResp))
	}
	commandResp, commandErr := probeAdminJSON(probeCtx, client, adminURL+"/panel/commands")
	if commandErr != nil {
		checks = append(checks, newDiagnosticCheck("admin.commands", "Admin /panel/commands", DiagnosticStatusWarning, "无法访问节点 /panel/commands", map[string]any{"url": adminURL + "/panel/commands", "error": commandErr.Error()}))
	} else {
		checks = append(checks, newDiagnosticCheck("admin.commands", "Admin /panel/commands", DiagnosticStatusOK, "节点命令状态接口可访问", map[string]any{"status_code": commandResp.StatusCode}))
	}
	checks = append(checks, probeNodeAdminSessions(probeCtx, client, adminURL))
	return checks
}

type adminProbeResponse struct {
	StatusCode int
	Body       map[string]any
}

func probeAdminJSON(ctx context.Context, client *http.Client, target string) (adminProbeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return adminProbeResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return adminProbeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return adminProbeResponse{StatusCode: resp.StatusCode}, fmt.Errorf("status %d", resp.StatusCode)
	}
	body := make(map[string]any)
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return adminProbeResponse{StatusCode: resp.StatusCode}, err
	}
	return adminProbeResponse{StatusCode: resp.StatusCode, Body: body}, nil
}

func nodeStatusProbeCheck(node Node, resp adminProbeResponse) DiagnosticCheck {
	level := DiagnosticStatusOK
	message := "节点 /status 可访问"
	detail := map[string]any{
		"status_code": resp.StatusCode,
	}
	if server, ok := resp.Body["server"].(map[string]any); ok {
		detail["server"] = server
		if listen, _ := server["listen"].(string); listen != "" {
			detail["listen"] = listen
		}
		if alpn, _ := server["alpn"].(string); strings.TrimSpace(node.ALPN) != "" && alpn != "" && alpn != node.ALPN {
			level = DiagnosticStatusWarning
			message = "节点 /status 可访问，但 ALPN 与面板节点配置不一致"
			detail["expected_alpn"] = node.ALPN
			detail["actual_alpn"] = alpn
		}
	}
	if nodeInfo, ok := resp.Body["node"].(map[string]any); ok {
		detail["node"] = nodeInfo
		if actualID, _ := nodeInfo["id"].(string); actualID != "" && actualID != node.NodeID {
			level = DiagnosticStatusWarning
			message = "节点 /status 可访问，但 node.id 与面板节点 ID 不一致"
			detail["expected_node_id"] = node.NodeID
			detail["actual_node_id"] = actualID
		}
	}
	if policies, ok := resp.Body["route_policies"].(map[string]any); ok {
		detail["route_policies"] = policies
	}
	return newDiagnosticCheck("admin.status", "Admin /status", level, message, detail)
}

func probeNodeAdminSessions(ctx context.Context, client *http.Client, adminURL string) DiagnosticCheck {
	resp, err := probeAdminJSON(ctx, client, adminURL+"/sessions")
	if err != nil {
		return newDiagnosticCheck("admin.sessions", "Admin /sessions", DiagnosticStatusWarning, "无法访问节点 /sessions，客户端联调观测会受限", map[string]any{
			"url":   adminURL + "/sessions",
			"error": err.Error(),
		})
	}
	return nodeSessionsProbeCheck(resp)
}

func nodeSessionsProbeCheck(resp adminProbeResponse) DiagnosticCheck {
	detail := map[string]any{
		"status_code": resp.StatusCode,
	}
	sessions, _ := resp.Body["sessions"].([]any)
	activeFlows := 0
	users := make(map[string]struct{})
	devices := make(map[string]struct{})
	games := make(map[string]struct{})
	policies := make(map[string]struct{})
	for _, item := range sessions {
		session, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if userID, _ := session["user_id"].(string); strings.TrimSpace(userID) != "" {
			users[userID] = struct{}{}
		}
		if deviceID, _ := session["device_id"].(string); strings.TrimSpace(deviceID) != "" {
			devices[deviceID] = struct{}{}
		}
		flows, _ := session["flows"].([]any)
		activeFlows += len(flows)
		for _, flowItem := range flows {
			flow, ok := flowItem.(map[string]any)
			if !ok {
				continue
			}
			if gameID, _ := flow["game_id"].(string); strings.TrimSpace(gameID) != "" {
				games[gameID] = struct{}{}
			}
			if policyID, _ := flow["policy_id"].(string); strings.TrimSpace(policyID) != "" {
				policies[policyID] = struct{}{}
			}
		}
	}
	detail["active_sessions"] = len(sessions)
	detail["active_flows"] = activeFlows
	detail["users"] = sortedMapKeys(users)
	detail["devices"] = sortedMapKeys(devices)
	detail["games"] = sortedMapKeys(games)
	detail["policies"] = sortedMapKeys(policies)
	return newDiagnosticCheck("admin.sessions", "Admin /sessions", DiagnosticStatusOK, "节点 /sessions 可访问，客户端会话观测可用", detail)
}

func sortedMapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func nodeRecommendations(checks []DiagnosticCheck, syncStatus NodeSyncStatus) []string {
	items := append([]string{}, syncStatus.Recommendations...)
	for _, check := range checks {
		if check.Status == DiagnosticStatusError || check.Status == DiagnosticStatusWarning {
			items = append(items, check.Message)
		}
	}
	return dedupeStrings(items)
}

func configuredCheck(key string, label string, ok bool, okMessage string, errorMessage string) DiagnosticCheck {
	if ok {
		return newDiagnosticCheck(key, label, DiagnosticStatusOK, okMessage, nil)
	}
	return newDiagnosticCheck(key, label, DiagnosticStatusError, errorMessage, nil)
}

func newDiagnosticCheck(key string, label string, status string, message string, detail map[string]any) DiagnosticCheck {
	if status == "" {
		status = DiagnosticStatusOK
	}
	return DiagnosticCheck{
		Key:     key,
		Label:   label,
		Status:  status,
		Message: message,
		Detail:  detail,
	}
}

func summarizeDiagnosticChecks(checks []DiagnosticCheck) DiagnosticSummary {
	summary := DiagnosticSummary{Total: len(checks)}
	for _, check := range checks {
		switch check.Status {
		case DiagnosticStatusError:
			summary.Error++
		case DiagnosticStatusWarning:
			summary.Warning++
		default:
			summary.OK++
		}
	}
	return summary
}

func overallDiagnosticStatus(summary DiagnosticSummary) string {
	if summary.Error > 0 {
		return DiagnosticStatusError
	}
	if summary.Warning > 0 {
		return DiagnosticStatusWarning
	}
	return DiagnosticStatusOK
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
