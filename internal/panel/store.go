package panel

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

type NodeListFilter struct {
	Query  string
	Status string
	Region string
	Limit  int
	Offset int
}

type AuditLog struct {
	OperatorID *uint64
	Action     string
	TargetType string
	TargetID   string
	Request    any
	IP         string
	UserAgent  string
}

type NodeStore interface {
	GetPanelUserByID(ctx context.Context, id uint64) (*PanelUser, error)
	GetPanelUserByUsername(ctx context.Context, username string) (*PanelUser, error)
	ListPanelUsers(ctx context.Context) ([]PanelUser, error)
	CreatePanelUser(ctx context.Context, username string, passwordHash string, role string, status string) (*PanelUser, error)
	UpdatePanelUser(ctx context.Context, id uint64, role string, status string) (*PanelUser, error)
	UpdatePanelUserPassword(ctx context.Context, id uint64, passwordHash string) (*PanelUser, error)
	ListNodes(ctx context.Context, filter NodeListFilter) ([]Node, error)
	GetNode(ctx context.Context, nodeID string) (*Node, error)
	UpsertNode(ctx context.Context, node Node) (*Node, error)
	DeleteNode(ctx context.Context, nodeID string) error
	UpsertPolicyRevision(ctx context.Context, input PolicyRevisionInput) (*PolicyRevision, error)
	ListPolicyRevisions(ctx context.Context, limit int) ([]PolicyRevision, error)
	GetPolicyRevision(ctx context.Context, revision string) (*PolicyRevision, error)
	SetNodeDesiredPolicy(ctx context.Context, nodeID string, revision string, now time.Time) (*NodePolicyRevision, error)
	GetNodePolicyRevision(ctx context.Context, nodeID string, revision string) (*NodePolicyRevision, error)
	GetTokenDefaults(ctx context.Context) (*TokenDefaults, error)
	SaveTokenDefaults(ctx context.Context, input TokenDefaultsInput) (*TokenDefaults, error)
	SaveNodeReport(ctx context.Context, report NodeReportInput) (*NodeReport, error)
	ListNodeReports(ctx context.Context, nodeID string, limit int) ([]NodeReport, error)
	ListClientSessions(ctx context.Context, filter ClientSessionFilter) (*ClientSessionList, error)
	GetTrafficOverview(ctx context.Context, filter TrafficOverviewFilter) (*TrafficOverview, error)
	CreateNodeTask(ctx context.Context, task NodeTaskInput) (*NodeTask, error)
	GetNodeTask(ctx context.Context, taskID string) (*NodeTask, error)
	ListNodeTasks(ctx context.Context, nodeID string, limit int) ([]NodeTask, error)
	ClaimPendingNodeTasks(ctx context.Context, nodeID string, limit int, now time.Time) ([]NodeTask, error)
	UpdateNodeTask(ctx context.Context, update NodeTaskUpdate) (*NodeTask, error)
	AppendNodeTaskLog(ctx context.Context, log NodeTaskLogInput) (*NodeTaskLog, error)
	ListNodeTaskLogs(ctx context.Context, taskID string, limit int) ([]NodeTaskLog, error)
	UpsertNodeCredential(ctx context.Context, credential NodeCredentialInput) (*NodeCredential, error)
	GetNodeCredential(ctx context.Context, nodeID string) (*NodeCredential, error)
	DeleteNodeCredential(ctx context.Context, nodeID string) error
	MarkNodeCredentialUsed(ctx context.Context, nodeID string, usedAt time.Time) error
	UpdateNodeOperationalState(ctx context.Context, nodeID string, status string, currentVersion string, lastError string) error
	RecordAudit(ctx context.Context, entry AuditLog) error
}

type NodeReportInput struct {
	Payload NodeReportPayload
	RawJSON json.RawMessage
}

type NodeReport struct {
	ID                    uint64          `json:"id"`
	NodeID                string          `json:"node_id"`
	Version               string          `json:"version"`
	Status                string          `json:"status"`
	RoutePolicyRevision   string          `json:"route_policy_revision"`
	RoutePolicyCount      int             `json:"route_policy_count"`
	ActiveQUICConnections int64           `json:"active_quic_connections"`
	ActiveTCPFlows        int64           `json:"active_tcp_flows"`
	ActiveUDPFlows        int64           `json:"active_udp_flows"`
	Metrics               json.RawMessage `json:"metrics_json,omitempty"`
	PanelCommands         json.RawMessage `json:"panel_commands_json,omitempty"`
	Raw                   json.RawMessage `json:"raw_json,omitempty"`
	ReportedAt            time.Time       `json:"reported_at"`
	CreatedAt             time.Time       `json:"created_at"`
}

type NodeTaskInput struct {
	TaskID      string
	NodeID      string
	Type        string
	Status      string
	Priority    int
	RequestJSON any
}

type NodeTask struct {
	ID           uint64          `json:"id"`
	TaskID       string          `json:"task_id"`
	NodeID       string          `json:"node_id"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	Priority     int             `json:"priority"`
	RequestJSON  json.RawMessage `json:"request_json,omitempty"`
	ResultJSON   json.RawMessage `json:"result_json,omitempty"`
	ErrorMessage string          `json:"error_message"`
	QueuedAt     time.Time       `json:"queued_at"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type NodeTaskUpdate struct {
	TaskID       string
	Status       string
	ResultJSON   any
	ErrorMessage string
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type NodeTaskLogInput struct {
	TaskID  string
	NodeID  string
	Step    string
	Stream  string
	Message string
}

type NodeTaskLog struct {
	ID        uint64    `json:"id"`
	TaskID    string    `json:"task_id"`
	NodeID    string    `json:"node_id"`
	Step      string    `json:"step"`
	Stream    string    `json:"stream"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}
