package panel

import (
	"strings"
	"time"
)

const (
	ClientSessionStatusOnline = "online"
	ClientSessionStatusClosed = "closed"
)

type ClientSessionFilter struct {
	NodeID      string
	UserID      string
	DeviceID    string
	Status      string
	CloseReason string
	WindowHours int
	Limit       int
	Offset      int
}

type ClientSessionList struct {
	Sessions []ClientSession       `json:"sessions"`
	Overview ClientSessionOverview `json:"overview"`
	Limit    int                   `json:"limit"`
	Offset   int                   `json:"offset"`
}

type ClientSessionOverview struct {
	OnlineSessions         int64 `json:"online_sessions"`
	ClosedSessions         int64 `json:"closed_sessions"`
	TimeoutSessions        int64 `json:"timeout_sessions"`
	TotalSessions          int64 `json:"total_sessions"`
	TotalDurationSeconds   int64 `json:"total_duration_seconds"`
	UDPClientToTargetBytes int64 `json:"udp_client_to_target_bytes"`
	UDPTargetToClientBytes int64 `json:"udp_target_to_client_bytes"`
	TCPClientToTargetBytes int64 `json:"tcp_client_to_target_bytes"`
	TCPTargetToClientBytes int64 `json:"tcp_target_to_client_bytes"`
}

type ClientSession struct {
	ID                     uint64     `json:"id"`
	NodeID                 string     `json:"node_id"`
	SessionID              string     `json:"session_id"`
	RemoteAddr             string     `json:"remote_addr"`
	UserID                 string     `json:"user_id"`
	DeviceID               string     `json:"device_id"`
	ClientID               string     `json:"client_id"`
	ClientVersion          string     `json:"client_version"`
	ClientPlatform         string     `json:"client_platform"`
	ProtocolVersion        int        `json:"protocol_version"`
	Status                 string     `json:"status"`
	CloseReason            string     `json:"close_reason"`
	CloseSource            string     `json:"close_source"`
	GameIDs                []string   `json:"game_ids"`
	PolicyIDs              []string   `json:"policy_ids"`
	ConfigRevision         string     `json:"config_revision"`
	ConnectedAt            time.Time  `json:"connected_at"`
	AuthenticatedAt        *time.Time `json:"authenticated_at,omitempty"`
	LastSeenAt             time.Time  `json:"last_seen_at"`
	LastPingAt             *time.Time `json:"last_ping_at,omitempty"`
	EndedAt                *time.Time `json:"ended_at,omitempty"`
	DurationSeconds        int64      `json:"duration_seconds"`
	MaxConnections         int        `json:"max_connections"`
	RateLimitMbps          int        `json:"rate_limit_mbps"`
	AllowTCP               bool       `json:"allow_tcp"`
	AllowUDP               bool       `json:"allow_udp"`
	UDPFlows               int        `json:"udp_flows"`
	TCPFlows               int        `json:"tcp_flows"`
	UDPClientToTargetBytes int64      `json:"udp_client_to_target_bytes"`
	UDPTargetToClientBytes int64      `json:"udp_target_to_client_bytes"`
	TCPClientToTargetBytes int64      `json:"tcp_client_to_target_bytes"`
	TCPTargetToClientBytes int64      `json:"tcp_target_to_client_bytes"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type ClientSessionEvent struct {
	ID          uint64    `json:"id"`
	NodeID      string    `json:"node_id"`
	SessionID   string    `json:"session_id"`
	EventType   string    `json:"event_type"`
	UserID      string    `json:"user_id"`
	DeviceID    string    `json:"device_id"`
	CloseReason string    `json:"close_reason"`
	CloseSource string    `json:"close_source"`
	OccurredAt  time.Time `json:"occurred_at"`
	PayloadJSON string    `json:"payload_json,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func normalizeClientSessionFilter(filter ClientSessionFilter) ClientSessionFilter {
	filter.NodeID = strings.TrimSpace(filter.NodeID)
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.DeviceID = strings.TrimSpace(filter.DeviceID)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.CloseReason = strings.TrimSpace(filter.CloseReason)
	if filter.WindowHours <= 0 {
		filter.WindowHours = 24
	}
	if filter.WindowHours > 24*90 {
		filter.WindowHours = 24 * 90
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}
