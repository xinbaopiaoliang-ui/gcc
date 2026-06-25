package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gaccel-node/internal/sessions"
)

const clientSessionSelectColumns = `
id, node_id, session_id, remote_addr, user_id, device_id, client_id,
client_version, client_platform, protocol_version, status, close_reason,
close_source, game_ids, policy_ids, config_revision, connected_at,
authenticated_at, last_seen_at, last_ping_at, ended_at, duration_seconds,
max_connections, rate_limit_mbps, allow_tcp, allow_udp, udp_flows, tcp_flows,
udp_client_to_target_bytes, udp_target_to_client_bytes, tcp_client_to_target_bytes,
tcp_target_to_client_bytes, created_at, updated_at`

func (s *MySQLStore) ListClientSessions(ctx context.Context, filter ClientSessionFilter) (*ClientSessionList, error) {
	filter = normalizeClientSessionFilter(filter)
	where, args := clientSessionWhere(filter)
	query := "SELECT " + clientSessionSelectColumns + " FROM panel_client_sessions"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY COALESCE(ended_at, last_seen_at, connected_at) DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ClientSession, 0)
	for rows.Next() {
		item, err := scanClientSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	overview, err := s.clientSessionOverview(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &ClientSessionList{
		Sessions: items,
		Overview: overview,
		Limit:    filter.Limit,
		Offset:   filter.Offset,
	}, nil
}

func (s *MySQLStore) clientSessionOverview(ctx context.Context, filter ClientSessionFilter) (ClientSessionOverview, error) {
	where, args := clientSessionWhere(filter)
	query := `
SELECT
  COALESCE(SUM(CASE WHEN status = 'online' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN close_reason IN ('heartbeat_timeout','quic_idle_timeout') THEN 1 ELSE 0 END), 0),
  COUNT(*),
  COALESCE(SUM(duration_seconds), 0),
  COALESCE(SUM(udp_client_to_target_bytes), 0),
  COALESCE(SUM(udp_target_to_client_bytes), 0),
  COALESCE(SUM(tcp_client_to_target_bytes), 0),
  COALESCE(SUM(tcp_target_to_client_bytes), 0)
FROM panel_client_sessions`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	var overview ClientSessionOverview
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&overview.OnlineSessions,
		&overview.ClosedSessions,
		&overview.TimeoutSessions,
		&overview.TotalSessions,
		&overview.TotalDurationSeconds,
		&overview.UDPClientToTargetBytes,
		&overview.UDPTargetToClientBytes,
		&overview.TCPClientToTargetBytes,
		&overview.TCPTargetToClientBytes,
	)
	return overview, err
}

func clientSessionWhere(filter ClientSessionFilter) ([]string, []any) {
	where := make([]string, 0, 8)
	args := make([]any, 0, 8)
	since := time.Now().UTC().Add(-time.Duration(filter.WindowHours) * time.Hour)
	where = append(where, "(status = 'online' OR connected_at >= ? OR last_seen_at >= ? OR ended_at >= ?)")
	args = append(args, since, since, since)
	if filter.NodeID != "" {
		where = append(where, "node_id = ?")
		args = append(args, filter.NodeID)
	}
	if filter.UserID != "" {
		where = append(where, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.DeviceID != "" {
		where = append(where, "device_id = ?")
		args = append(args, filter.DeviceID)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.CloseReason != "" {
		where = append(where, "close_reason = ?")
		args = append(args, filter.CloseReason)
	}
	return where, args
}

func saveClientSessionsFromReport(ctx context.Context, tx *sql.Tx, nodeID string, payload NodeReportPayload, reportedAt time.Time) error {
	for _, snapshot := range payload.Sessions {
		if err := upsertClientSessionSnapshot(ctx, tx, nodeID, snapshot, reportedAt); err != nil {
			return err
		}
	}
	for _, event := range payload.SessionEvents {
		if err := insertClientSessionEvent(ctx, tx, nodeID, event, reportedAt); err != nil {
			return err
		}
		if err := upsertClientSessionEventState(ctx, tx, nodeID, event, reportedAt); err != nil {
			return err
		}
	}
	return nil
}

func upsertClientSessionSnapshot(ctx context.Context, tx *sql.Tx, nodeID string, snapshot sessions.Snapshot, reportedAt time.Time) error {
	if strings.TrimSpace(snapshot.ID) == "" {
		return nil
	}
	gameIDs, err := jsonString(nonNilStrings(snapshot.GameIDs))
	if err != nil {
		return err
	}
	policyIDs, err := jsonString(nonNilStrings(snapshot.PolicyIDs))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO panel_client_sessions (
  node_id, session_id, remote_addr, user_id, device_id, client_id,
  client_version, client_platform, protocol_version, status, game_ids, policy_ids,
  config_revision, connected_at, authenticated_at, last_seen_at, last_ping_at,
  duration_seconds, max_connections, rate_limit_mbps, allow_tcp, allow_udp,
  udp_flows, tcp_flows, udp_client_to_target_bytes, udp_target_to_client_bytes,
  tcp_client_to_target_bytes, tcp_target_to_client_bytes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'online', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  remote_addr = VALUES(remote_addr),
  user_id = VALUES(user_id),
  device_id = VALUES(device_id),
  client_id = VALUES(client_id),
  client_version = VALUES(client_version),
  client_platform = VALUES(client_platform),
  protocol_version = VALUES(protocol_version),
  status = 'online',
  close_reason = '',
  close_source = '',
  game_ids = VALUES(game_ids),
  policy_ids = VALUES(policy_ids),
  config_revision = VALUES(config_revision),
  authenticated_at = COALESCE(VALUES(authenticated_at), authenticated_at),
  last_seen_at = VALUES(last_seen_at),
  last_ping_at = VALUES(last_ping_at),
  ended_at = NULL,
  duration_seconds = VALUES(duration_seconds),
  max_connections = VALUES(max_connections),
  rate_limit_mbps = VALUES(rate_limit_mbps),
  allow_tcp = VALUES(allow_tcp),
  allow_udp = VALUES(allow_udp),
  udp_flows = VALUES(udp_flows),
  tcp_flows = VALUES(tcp_flows),
  udp_client_to_target_bytes = VALUES(udp_client_to_target_bytes),
  udp_target_to_client_bytes = VALUES(udp_target_to_client_bytes),
  tcp_client_to_target_bytes = VALUES(tcp_client_to_target_bytes),
  tcp_target_to_client_bytes = VALUES(tcp_target_to_client_bytes),
  updated_at = CURRENT_TIMESTAMP`,
		nodeID, snapshot.ID, snapshot.RemoteAddr, snapshot.UserID, snapshot.DeviceID, snapshot.ClientID,
		snapshot.ClientVersion, snapshot.ClientPlatform, snapshot.ProtocolVersion, gameIDs, policyIDs,
		snapshot.ConfigRevision, snapshot.CreatedAt, snapshot.AuthenticatedAt, snapshot.LastSeen,
		snapshot.LastPingAt, snapshot.ConnectedDurationSeconds, snapshot.MaxConns, snapshot.RateLimitMbps,
		snapshot.AllowTCP, snapshot.AllowUDP, snapshot.UDPFlows, snapshot.TCPFlows,
		snapshot.UDPClientToTargetBytes, snapshot.UDPTargetToClientBytes,
		snapshot.TCPClientToTargetBytes, snapshot.TCPTargetToClientBytes,
	)
	if err != nil {
		return fmt.Errorf("upsert client session %s/%s: %w", nodeID, snapshot.ID, err)
	}
	_ = reportedAt
	return nil
}

func insertClientSessionEvent(ctx context.Context, tx *sql.Tx, nodeID string, event sessions.Event, reportedAt time.Time) error {
	if strings.TrimSpace(event.SessionID) == "" || strings.TrimSpace(event.Type) == "" {
		return nil
	}
	occurredAt := eventTime(event, reportedAt)
	payload, err := jsonString(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO panel_client_session_events (
  node_id, session_id, event_type, user_id, device_id, close_reason, close_source,
  occurred_at, payload_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID, event.SessionID, event.Type, event.UserID, event.DeviceID,
		event.CloseReason, event.CloseSource, occurredAt, payload,
	)
	if err != nil {
		return fmt.Errorf("insert client session event %s/%s/%s: %w", nodeID, event.SessionID, event.Type, err)
	}
	return nil
}

func upsertClientSessionEventState(ctx context.Context, tx *sql.Tx, nodeID string, event sessions.Event, reportedAt time.Time) error {
	if strings.TrimSpace(event.SessionID) == "" {
		return nil
	}
	gameIDs, err := jsonString(nonNilStrings(event.GameIDs))
	if err != nil {
		return err
	}
	policyIDs, err := jsonString(nonNilStrings(event.PolicyIDs))
	if err != nil {
		return err
	}
	status := event.Status
	if status == "" {
		status = "online"
	}
	occurredAt := eventTime(event, reportedAt)
	connectedAt := event.ConnectedAt
	if connectedAt.IsZero() {
		connectedAt = occurredAt
	}
	lastSeen := event.LastSeenAt
	if lastSeen.IsZero() {
		lastSeen = occurredAt
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO panel_client_sessions (
  node_id, session_id, remote_addr, user_id, device_id, client_id,
  client_version, client_platform, protocol_version, status, close_reason, close_source,
  game_ids, policy_ids, config_revision, connected_at, authenticated_at, last_seen_at,
  last_ping_at, ended_at, duration_seconds, allow_tcp, allow_udp, udp_flows, tcp_flows,
  udp_client_to_target_bytes, udp_target_to_client_bytes, tcp_client_to_target_bytes,
  tcp_target_to_client_bytes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  remote_addr = COALESCE(NULLIF(VALUES(remote_addr), ''), remote_addr),
  user_id = COALESCE(NULLIF(VALUES(user_id), ''), user_id),
  device_id = COALESCE(NULLIF(VALUES(device_id), ''), device_id),
  client_id = COALESCE(NULLIF(VALUES(client_id), ''), client_id),
  client_version = COALESCE(NULLIF(VALUES(client_version), ''), client_version),
  client_platform = COALESCE(NULLIF(VALUES(client_platform), ''), client_platform),
  protocol_version = IF(VALUES(protocol_version) = 0, protocol_version, VALUES(protocol_version)),
  status = VALUES(status),
  close_reason = VALUES(close_reason),
  close_source = VALUES(close_source),
  game_ids = VALUES(game_ids),
  policy_ids = VALUES(policy_ids),
  config_revision = COALESCE(NULLIF(VALUES(config_revision), ''), config_revision),
  authenticated_at = COALESCE(VALUES(authenticated_at), authenticated_at),
  last_seen_at = VALUES(last_seen_at),
  last_ping_at = COALESCE(VALUES(last_ping_at), last_ping_at),
  ended_at = VALUES(ended_at),
  duration_seconds = GREATEST(duration_seconds, VALUES(duration_seconds)),
  udp_flows = VALUES(udp_flows),
  tcp_flows = VALUES(tcp_flows),
  udp_client_to_target_bytes = GREATEST(udp_client_to_target_bytes, VALUES(udp_client_to_target_bytes)),
  udp_target_to_client_bytes = GREATEST(udp_target_to_client_bytes, VALUES(udp_target_to_client_bytes)),
  tcp_client_to_target_bytes = GREATEST(tcp_client_to_target_bytes, VALUES(tcp_client_to_target_bytes)),
  tcp_target_to_client_bytes = GREATEST(tcp_target_to_client_bytes, VALUES(tcp_target_to_client_bytes)),
  updated_at = CURRENT_TIMESTAMP`,
		nodeID, event.SessionID, event.RemoteAddr, event.UserID, event.DeviceID, event.ClientID,
		event.ClientVersion, event.ClientPlatform, event.ProtocolVersion, status, event.CloseReason, event.CloseSource,
		gameIDs, policyIDs, event.ConfigRevision, connectedAt, event.AuthenticatedAt, lastSeen,
		event.LastPingAt, event.EndedAt, event.DurationSeconds, event.UDPFlows, event.TCPFlows,
		event.UDPClientToTargetBytes, event.UDPTargetToClientBytes,
		event.TCPClientToTargetBytes, event.TCPTargetToClientBytes,
	)
	if err != nil {
		return fmt.Errorf("upsert client session event state %s/%s: %w", nodeID, event.SessionID, err)
	}
	return nil
}

func eventTime(event sessions.Event, fallback time.Time) time.Time {
	if event.EndedAt != nil && !event.EndedAt.IsZero() {
		return *event.EndedAt
	}
	if event.AuthenticatedAt != nil && !event.AuthenticatedAt.IsZero() {
		return *event.AuthenticatedAt
	}
	if !event.LastSeenAt.IsZero() {
		return event.LastSeenAt
	}
	if !event.ConnectedAt.IsZero() {
		return event.ConnectedAt
	}
	if fallback.IsZero() {
		return time.Now().UTC()
	}
	return fallback
}

func scanClientSession(scanner rowScanner) (ClientSession, error) {
	var item ClientSession
	var gameIDsRaw sql.NullString
	var policyIDsRaw sql.NullString
	var authenticatedAt sql.NullTime
	var lastPingAt sql.NullTime
	var endedAt sql.NullTime
	err := scanner.Scan(
		&item.ID, &item.NodeID, &item.SessionID, &item.RemoteAddr, &item.UserID, &item.DeviceID, &item.ClientID,
		&item.ClientVersion, &item.ClientPlatform, &item.ProtocolVersion, &item.Status, &item.CloseReason,
		&item.CloseSource, &gameIDsRaw, &policyIDsRaw, &item.ConfigRevision, &item.ConnectedAt,
		&authenticatedAt, &item.LastSeenAt, &lastPingAt, &endedAt, &item.DurationSeconds,
		&item.MaxConnections, &item.RateLimitMbps, &item.AllowTCP, &item.AllowUDP, &item.UDPFlows, &item.TCPFlows,
		&item.UDPClientToTargetBytes, &item.UDPTargetToClientBytes, &item.TCPClientToTargetBytes,
		&item.TCPTargetToClientBytes, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return ClientSession{}, err
	}
	item.GameIDs = decodeStringArray(gameIDsRaw)
	item.PolicyIDs = decodeStringArray(policyIDsRaw)
	if authenticatedAt.Valid {
		item.AuthenticatedAt = &authenticatedAt.Time
	}
	if lastPingAt.Valid {
		item.LastPingAt = &lastPingAt.Time
	}
	if endedAt.Valid {
		item.EndedAt = &endedAt.Time
	}
	return item, nil
}

func decodeStringArray(raw sql.NullString) []string {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil {
		return []string{}
	}
	return nonNilStrings(values)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
