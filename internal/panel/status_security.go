package panel

import (
	"strings"
	"time"
)

type NodeSyncStatus struct {
	NodeID                string     `json:"node_id"`
	Status                string     `json:"status"`
	CurrentVersion        string     `json:"current_version"`
	DesiredVersion        string     `json:"desired_version"`
	VersionState          string     `json:"version_state"`
	CurrentPolicyRevision string     `json:"current_policy_revision"`
	DesiredPolicyRevision string     `json:"desired_policy_revision"`
	PolicyState           string     `json:"policy_state"`
	LastReportAt          *time.Time `json:"last_report_at,omitempty"`
	ReportAgeSeconds      *int64     `json:"report_age_seconds,omitempty"`
	LastError             string     `json:"last_error"`
	PendingTasks          int        `json:"pending_tasks"`
	RunningTasks          int        `json:"running_tasks"`
	FailedTasks           int        `json:"failed_tasks"`
	LatestTask            *NodeTask  `json:"latest_task,omitempty"`
	HMACSecretConfigured  bool       `json:"hmac_secret_configured"`
	HMACSecretSource      string     `json:"hmac_secret_source,omitempty"`
	HMACSecretUpdatedAt   *time.Time `json:"hmac_secret_updated_at,omitempty"`
	DeployReady           bool       `json:"deploy_ready"`
	Recommendations       []string   `json:"recommendations"`
}

type SecurityOverview struct {
	Users    SecurityUserSummary `json:"users"`
	Nodes    SecurityNodeSummary `json:"nodes"`
	Config   SecurityConfigState `json:"config"`
	Warnings []string            `json:"warnings"`
}

type SecurityUserSummary struct {
	Total    int `json:"total"`
	Admins   int `json:"admins"`
	Active   int `json:"active"`
	Disabled int `json:"disabled"`
}

type SecurityNodeSummary struct {
	Total              int `json:"total"`
	WithCredentials    int `json:"with_credentials"`
	WithoutCredentials int `json:"without_credentials"`
	WithoutHMACSecret  int `json:"without_hmac_secret"`
	Disabled           int `json:"disabled"`
	OfflineOrError     int `json:"offline_or_error"`
	PolicyDrift        int `json:"policy_drift"`
	VersionDrift       int `json:"version_drift"`
}

type SecurityConfigState struct {
	Listen                  string   `json:"listen"`
	PublicBaseURL           string   `json:"public_base_url,omitempty"`
	SessionTTLSeconds       int64    `json:"session_ttl_seconds"`
	BackendAPIKeyCount      int      `json:"backend_api_key_count"`
	MasterKeyConfigured     bool     `json:"master_key_configured"`
	SessionSecretConfigured bool     `json:"session_secret_configured"`
	CommandSecretConfigured bool     `json:"command_secret_configured"`
	CORSAllowedOrigins      []string `json:"cors_allowed_origins"`
}

func BuildNodeSyncStatus(node Node, reports []NodeReport, tasks []NodeTask, now time.Time) NodeSyncStatus {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := NodeSyncStatus{
		NodeID:                node.NodeID,
		Status:                node.Status,
		CurrentVersion:        node.CurrentVersion,
		DesiredVersion:        node.DesiredVersion,
		VersionState:          compareDesiredState(node.CurrentVersion, node.DesiredVersion),
		CurrentPolicyRevision: node.CurrentPolicyRevision,
		DesiredPolicyRevision: node.DesiredPolicyRevision,
		PolicyState:           compareDesiredState(node.CurrentPolicyRevision, node.DesiredPolicyRevision),
		LastReportAt:          node.LastReportAt,
		LastError:             strings.TrimSpace(node.LastError),
		HMACSecretConfigured:  node.HMACSecretConfigured,
		HMACSecretSource:      node.HMACSecretSource,
		HMACSecretUpdatedAt:   node.HMACSecretUpdatedAt,
		DeployReady:           node.HMACSecretConfigured,
	}
	if status.LastReportAt == nil && len(reports) > 0 {
		status.LastReportAt = &reports[0].ReportedAt
	}
	if status.LastReportAt != nil {
		age := int64(now.Sub(status.LastReportAt.UTC()).Seconds())
		if age < 0 {
			age = 0
		}
		status.ReportAgeSeconds = &age
	}
	for i := range tasks {
		task := tasks[i]
		switch task.Status {
		case TaskStatusPending:
			status.PendingTasks++
		case TaskStatusRunning:
			status.RunningTasks++
		case TaskStatusFailed:
			status.FailedTasks++
		}
		if status.LatestTask == nil || task.QueuedAt.After(status.LatestTask.QueuedAt) {
			copied := task
			status.LatestTask = &copied
		}
	}
	status.Recommendations = syncRecommendations(status)
	return status
}

func compareDesiredState(currentValue, desiredValue string) string {
	current := strings.TrimSpace(currentValue)
	desired := strings.TrimSpace(desiredValue)
	if desired == "" {
		if current == "" {
			return "unknown"
		}
		return "not_set"
	}
	if current == desired {
		return "synced"
	}
	if current == "" {
		return "waiting_report"
	}
	return "pending"
}

func syncRecommendations(status NodeSyncStatus) []string {
	items := make([]string, 0)
	if status.LastReportAt == nil {
		items = append(items, "节点还没有上报，请检查 panel.report_url、API Key 和节点服务状态")
	} else if status.ReportAgeSeconds != nil && *status.ReportAgeSeconds > 180 {
		items = append(items, "节点上报已超过 180 秒，请检查节点到控制面板的网络和定时上报进程")
	}
	if status.PolicyState == "pending" || status.PolicyState == "waiting_report" {
		items = append(items, "策略未同步到目标版本，可查看 apply_policy 任务日志或等待节点下一次拉取命令")
	}
	if status.VersionState == "pending" || status.VersionState == "waiting_report" {
		items = append(items, "节点版本未达到目标版本，可创建更新任务或查看 update_node 日志")
	}
	if status.FailedTasks > 0 {
		items = append(items, "存在失败任务，可在任务日志中查看原因后执行重试")
	}
	if !status.HMACSecretConfigured {
		items = append(items, "节点 HMAC Secret 尚未由业务后台同步，无法一键部署或签发可用客户端 token")
	}
	if status.LastError != "" {
		items = append(items, "节点最后错误不为空，请优先排查该错误")
	}
	return items
}

func BuildSecurityOverview(cfg *Config, users []PanelUser, nodes []Node) SecurityOverview {
	overview := SecurityOverview{
		Warnings: make([]string, 0),
		Config: SecurityConfigState{
			Listen:                  cfg.Listen,
			PublicBaseURL:           cfg.PublicBaseURL,
			SessionTTLSeconds:       int64(cfg.Session.TTL.Seconds()),
			BackendAPIKeyCount:      len(cfg.Security.BackendAPIKeys),
			MasterKeyConfigured:     strings.TrimSpace(cfg.Security.MasterKey) != "",
			SessionSecretConfigured: strings.TrimSpace(cfg.Session.Secret) != "",
			CommandSecretConfigured: strings.TrimSpace(cfg.NodeCommand.Secret) != "",
			CORSAllowedOrigins:      append([]string{}, cfg.CORS.AllowedOrigins...),
		},
	}
	for _, user := range users {
		overview.Users.Total++
		if user.Role == PanelUserRoleAdmin {
			overview.Users.Admins++
		}
		if user.Status == PanelUserStatusActive {
			overview.Users.Active++
		}
		if user.Status == PanelUserStatusDisabled {
			overview.Users.Disabled++
		}
	}
	for _, node := range nodes {
		overview.Nodes.Total++
		if node.Status == "disabled" {
			overview.Nodes.Disabled++
		}
		if node.Status == "offline" || node.Status == "error" {
			overview.Nodes.OfflineOrError++
		}
		if compareDesiredState(node.CurrentPolicyRevision, node.DesiredPolicyRevision) == "pending" {
			overview.Nodes.PolicyDrift++
		}
		if compareDesiredState(node.CurrentVersion, node.DesiredVersion) == "pending" {
			overview.Nodes.VersionDrift++
		}
		if !node.HMACSecretConfigured {
			overview.Nodes.WithoutHMACSecret++
		}
	}
	if overview.Users.Admins == 0 {
		overview.Warnings = append(overview.Warnings, "没有启用管理员账号")
	}
	if overview.Config.BackendAPIKeyCount == 0 {
		overview.Warnings = append(overview.Warnings, "未配置业务后台 API Key")
	}
	if !overview.Config.CommandSecretConfigured {
		overview.Warnings = append(overview.Warnings, "未配置节点命令签名 secret")
	}
	if overview.Nodes.WithoutHMACSecret > 0 {
		overview.Warnings = append(overview.Warnings, "存在未同步节点 HMAC Secret 的节点，请由业务后台生成并同步后再部署")
	}
	if len(overview.Config.CORSAllowedOrigins) == 0 {
		overview.Warnings = append(overview.Warnings, "未配置 CORS 来源，前后端分离部署可能无法访问")
	}
	return overview
}
