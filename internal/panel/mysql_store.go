package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gaccel-node/internal/panelcommand"
	"gaccel-node/internal/systemstats"

	_ "github.com/go-sql-driver/mysql"
)

const nodeSelectColumns = `
id, node_id, name, region, country, provider, line_type,
endpoint_host, endpoint_port, alpn, admin_host, admin_port,
ssh_host, ssh_port, ssh_user, allow_tcp, allow_udp,
tags, labels, hmac_secret_encrypted, hmac_secret_source, hmac_secret_updated_at,
status, current_version, desired_version, current_policy_revision,
desired_policy_revision, last_report_at, last_error, created_at, updated_at`

type MySQLStore struct {
	db *sql.DB
}

func OpenMySQLStore(ctx context.Context, dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) Close() error {
	return s.db.Close()
}

func (s *MySQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *MySQLStore) CheckRequiredTables(ctx context.Context, tables []string) ([]SchemaTableCheck, error) {
	results := make([]SchemaTableCheck, 0, len(tables))
	for _, table := range tables {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		var count int
		if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count); err != nil {
			return nil, err
		}
		results = append(results, SchemaTableCheck{Name: table, Exists: count > 0})
	}
	return results, nil
}

func (s *MySQLStore) GetPanelUserByID(ctx context.Context, id uint64) (*PanelUser, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, role, status, created_at, updated_at
FROM panel_users
WHERE id = ?`, id)
	user, err := scanPanelUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *MySQLStore) GetPanelUserByUsername(ctx context.Context, username string) (*PanelUser, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, role, status, created_at, updated_at
FROM panel_users
WHERE username = ?`, strings.TrimSpace(username))
	user, err := scanPanelUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *MySQLStore) ListPanelUsers(ctx context.Context) ([]PanelUser, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, username, password_hash, role, status, created_at, updated_at
FROM panel_users
ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]PanelUser, 0)
	for rows.Next() {
		user, err := scanPanelUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *MySQLStore) CreatePanelUser(ctx context.Context, username string, passwordHash string, role string, status string) (*PanelUser, error) {
	username = strings.TrimSpace(username)
	passwordHash = strings.TrimSpace(passwordHash)
	if err := validatePanelUsername(username); err != nil {
		return nil, err
	}
	if passwordHash == "" {
		return nil, errors.New("password hash is required")
	}
	normalizedRole, err := normalizePanelRole(role)
	if err != nil {
		return nil, err
	}
	normalizedStatus, err := normalizePanelUserStatus(status)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetPanelUserByUsername(ctx, username); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO panel_users (username, password_hash, role, status)
VALUES (?, ?, ?, ?)`,
		username, passwordHash, normalizedRole, normalizedStatus,
	)
	if err != nil {
		return nil, err
	}
	return s.GetPanelUserByUsername(ctx, username)
}

func (s *MySQLStore) UpsertPanelUser(ctx context.Context, username string, passwordHash string, role string, status string) (*PanelUser, error) {
	username = strings.TrimSpace(username)
	passwordHash = strings.TrimSpace(passwordHash)
	if err := validatePanelUsername(username); err != nil {
		return nil, err
	}
	if passwordHash == "" {
		return nil, errors.New("password hash is required")
	}
	normalizedRole, err := normalizePanelRole(role)
	if err != nil {
		return nil, err
	}
	normalizedStatus, err := normalizePanelUserStatus(status)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO panel_users (username, password_hash, role, status)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  password_hash = VALUES(password_hash),
  role = VALUES(role),
  status = VALUES(status),
  updated_at = CURRENT_TIMESTAMP`,
		username, passwordHash, normalizedRole, normalizedStatus,
	)
	if err != nil {
		return nil, err
	}
	return s.GetPanelUserByUsername(ctx, username)
}

func (s *MySQLStore) UpdatePanelUser(ctx context.Context, id uint64, role string, status string) (*PanelUser, error) {
	if id == 0 {
		return nil, errors.New("panel user id is required")
	}
	normalizedRole, err := normalizePanelRole(role)
	if err != nil {
		return nil, err
	}
	normalizedStatus, err := normalizePanelUserStatus(status)
	if err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE panel_users
SET role = ?, status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, normalizedRole, normalizedStatus, id)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return nil, ErrNotFound
	}
	return s.GetPanelUserByID(ctx, id)
}

func (s *MySQLStore) UpdatePanelUserPassword(ctx context.Context, id uint64, passwordHash string) (*PanelUser, error) {
	passwordHash = strings.TrimSpace(passwordHash)
	if id == 0 {
		return nil, errors.New("panel user id is required")
	}
	if passwordHash == "" {
		return nil, errors.New("password hash is required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE panel_users
SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?`, passwordHash, id, PanelUserStatusActive)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return nil, ErrNotFound
	}
	return s.GetPanelUserByID(ctx, id)
}

func (s *MySQLStore) ListNodes(ctx context.Context, filter NodeListFilter) ([]Node, error) {
	where := make([]string, 0, 3)
	args := make([]any, 0, 8)
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		where = append(where, "(node_id LIKE ? OR name LIKE ? OR endpoint_host LIKE ? OR ssh_host LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Region != "" {
		where = append(where, "region = ?")
		args = append(args, filter.Region)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := "SELECT " + nodeSelectColumns + " FROM panel_nodes"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachLatestSystemSnapshots(ctx, nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *MySQLStore) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+" FROM panel_nodes WHERE node_id = ?", nodeID)
	node, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	nodes := []Node{node}
	if err := s.attachLatestSystemSnapshots(ctx, nodes); err != nil {
		return nil, err
	}
	node = nodes[0]
	return &node, nil
}

func (s *MySQLStore) attachLatestSystemSnapshots(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(nodes))
	args := make([]any, 0, len(nodes))
	indexByNodeID := make(map[string]int, len(nodes))
	for i := range nodes {
		nodeID := strings.TrimSpace(nodes[i].NodeID)
		if nodeID == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, nodeID)
		indexByNodeID[nodeID] = i
	}
	if len(args) == 0 {
		return nil
	}
	query := `
SELECT node_id, raw_json
FROM (
  SELECT node_id, raw_json, ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY reported_at DESC, id DESC) AS rn
  FROM panel_node_reports
  WHERE node_id IN (` + strings.Join(placeholders, ",") + `)
) latest
WHERE rn = 1`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var raw string
		if err := rows.Scan(&nodeID, &raw); err != nil {
			return err
		}
		idx, ok := indexByNodeID[nodeID]
		if !ok {
			continue
		}
		snapshot := extractSystemSnapshot([]byte(raw))
		if snapshot != nil {
			nodes[idx].LatestSystem = snapshot
		}
	}
	return rows.Err()
}

func extractSystemSnapshot(raw []byte) *systemstats.Snapshot {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		System *systemstats.Snapshot `json:"system"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.System == nil || !payload.System.HasData() {
		return nil
	}
	return payload.System
}

func (s *MySQLStore) UpsertNode(ctx context.Context, node Node) (*Node, error) {
	if err := ValidateNode(node); err != nil {
		return nil, err
	}
	tagsJSON, err := jsonString(node.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}
	labelsJSON, err := jsonString(node.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	hmacSecretEncrypted := strings.TrimSpace(node.HMACSecretEncrypted)
	hmacSecretSource := strings.TrimSpace(node.HMACSecretSource)
	if hmacSecretSource == "" && hmacSecretEncrypted != "" {
		hmacSecretSource = "backend"
	}
	var hmacSecretUpdatedAt any
	if hmacSecretEncrypted != "" {
		if node.HMACSecretUpdatedAt != nil {
			hmacSecretUpdatedAt = *node.HMACSecretUpdatedAt
		} else {
			hmacSecretUpdatedAt = time.Now().UTC()
		}
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO panel_nodes (
  node_id, name, region, country, provider, line_type,
  endpoint_host, endpoint_port, alpn, admin_host, admin_port,
  ssh_host, ssh_port, ssh_user, allow_tcp, allow_udp, tags, labels,
  hmac_secret_encrypted, hmac_secret_source, hmac_secret_updated_at,
  status, desired_version, desired_policy_revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  region = VALUES(region),
  country = VALUES(country),
  provider = VALUES(provider),
  line_type = VALUES(line_type),
  endpoint_host = VALUES(endpoint_host),
  endpoint_port = VALUES(endpoint_port),
  alpn = VALUES(alpn),
  admin_host = VALUES(admin_host),
  admin_port = VALUES(admin_port),
  ssh_host = VALUES(ssh_host),
  ssh_port = VALUES(ssh_port),
  ssh_user = VALUES(ssh_user),
  allow_tcp = VALUES(allow_tcp),
  allow_udp = VALUES(allow_udp),
  tags = VALUES(tags),
  labels = VALUES(labels),
  hmac_secret_encrypted = COALESCE(VALUES(hmac_secret_encrypted), hmac_secret_encrypted),
  hmac_secret_source = CASE
    WHEN VALUES(hmac_secret_encrypted) IS NULL THEN hmac_secret_source
    ELSE VALUES(hmac_secret_source)
  END,
  hmac_secret_updated_at = CASE
    WHEN VALUES(hmac_secret_encrypted) IS NULL THEN hmac_secret_updated_at
    ELSE VALUES(hmac_secret_updated_at)
  END,
  status = VALUES(status),
  desired_version = VALUES(desired_version),
  desired_policy_revision = VALUES(desired_policy_revision),
  updated_at = CURRENT_TIMESTAMP`,
		node.NodeID, node.Name, node.Region, node.Country, node.Provider, node.LineType,
		node.EndpointHost, node.EndpointPort, node.ALPN, node.AdminHost, node.AdminPort,
		node.SSHHost, node.SSHPort, node.SSHUser, node.AllowTCP, node.AllowUDP, tagsJSON, labelsJSON,
		nullIfEmpty(hmacSecretEncrypted), hmacSecretSource, hmacSecretUpdatedAt,
		node.Status, node.DesiredVersion, node.DesiredPolicyRevision,
	)
	if err != nil {
		return nil, err
	}
	return s.GetNode(ctx, node.NodeID)
}

func (s *MySQLStore) SetNodeHMACSecret(ctx context.Context, nodeID string, encryptedSecret string, source string, updatedAt time.Time) (*Node, error) {
	nodeID = strings.TrimSpace(nodeID)
	encryptedSecret = strings.TrimSpace(encryptedSecret)
	source = strings.TrimSpace(source)
	if source == "" {
		source = "panel"
	}
	if encryptedSecret == "" {
		return nil, errors.New("encrypted hmac secret is required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE panel_nodes
SET hmac_secret_encrypted = ?,
    hmac_secret_source = ?,
    hmac_secret_updated_at = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE node_id = ?`,
		encryptedSecret, source, updatedAt, nodeID,
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return s.GetNode(ctx, nodeID)
}

func (s *MySQLStore) ClearNodeHMACSecret(ctx context.Context, nodeID string) (*Node, error) {
	nodeID = strings.TrimSpace(nodeID)
	result, err := s.db.ExecContext(ctx, `
UPDATE panel_nodes
SET hmac_secret_encrypted = NULL,
    hmac_secret_source = '',
    hmac_secret_updated_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return s.GetNode(ctx, nodeID)
}

func (s *MySQLStore) DeleteNode(ctx context.Context, nodeID string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM panel_nodes WHERE node_id = ?", nodeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQLStore) UpsertPolicyRevision(ctx context.Context, input PolicyRevisionInput) (*PolicyRevision, error) {
	policy, err := NewPolicyRevisionFromInput(input)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO panel_policy_revisions (revision, sha256, route_policies_yaml, source)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  sha256 = VALUES(sha256),
  route_policies_yaml = VALUES(route_policies_yaml),
  source = VALUES(source)`,
		policy.Revision, policy.SHA256, policy.RoutePoliciesYAML, policy.Source,
	)
	if err != nil {
		return nil, err
	}
	return s.GetPolicyRevision(ctx, policy.Revision)
}

func (s *MySQLStore) ListPolicyRevisions(ctx context.Context, limit int) ([]PolicyRevision, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, revision, sha256, route_policies_yaml, source, created_at
FROM panel_policy_revisions
ORDER BY id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := make([]PolicyRevision, 0)
	for rows.Next() {
		policy, err := scanPolicyRevision(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return policies, nil
}

func (s *MySQLStore) GetPolicyRevision(ctx context.Context, revision string) (*PolicyRevision, error) {
	revision = strings.TrimSpace(revision)
	if err := validatePolicyRevisionID(revision); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, revision, sha256, route_policies_yaml, source, created_at
FROM panel_policy_revisions
WHERE revision = ?`, revision)
	policy, err := scanPolicyRevision(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &policy, nil
}

func (s *MySQLStore) SetNodeDesiredPolicy(ctx context.Context, nodeID string, revision string, now time.Time) (*NodePolicyRevision, error) {
	nodeID = strings.TrimSpace(nodeID)
	revision = strings.TrimSpace(revision)
	if !nodeIDPattern.MatchString(nodeID) {
		return nil, errors.New("node_id is invalid")
	}
	if err := validatePolicyRevisionID(revision); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var currentPolicyRevision string
	err = tx.QueryRowContext(ctx, `
SELECT current_policy_revision
FROM panel_nodes
WHERE node_id = ?
FOR UPDATE`, nodeID).Scan(&currentPolicyRevision)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	applied := strings.TrimSpace(currentPolicyRevision) == revision
	var appliedAt any
	if applied {
		appliedAt = now
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE panel_nodes
SET desired_policy_revision = ?, updated_at = CURRENT_TIMESTAMP
WHERE node_id = ?`, revision, nodeID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE panel_node_policy_revisions
SET desired = 0, updated_at = CURRENT_TIMESTAMP
WHERE node_id = ? AND revision <> ?`, nodeID, revision); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO panel_node_policy_revisions (
  node_id, revision, desired, applied, applied_at, last_error
) VALUES (?, ?, 1, ?, ?, '')
ON DUPLICATE KEY UPDATE
  desired = 1,
  applied = VALUES(applied),
  applied_at = VALUES(applied_at),
  last_error = '',
  updated_at = CURRENT_TIMESTAMP`,
		nodeID, revision, applied, appliedAt,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetNodePolicyRevision(ctx, nodeID, revision)
}

func (s *MySQLStore) GetNodePolicyRevision(ctx context.Context, nodeID string, revision string) (*NodePolicyRevision, error) {
	nodeID = strings.TrimSpace(nodeID)
	revision = strings.TrimSpace(revision)
	if !nodeIDPattern.MatchString(nodeID) {
		return nil, errors.New("node_id is invalid")
	}
	if err := validatePolicyRevisionID(revision); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, node_id, revision, desired, applied, applied_at, last_error, created_at, updated_at
FROM panel_node_policy_revisions
WHERE node_id = ? AND revision = ?`, nodeID, revision)
	nodePolicy, err := scanNodePolicyRevision(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &nodePolicy, nil
}

func (s *MySQLStore) GetTokenDefaults(ctx context.Context) (*TokenDefaults, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT plan_id, name, max_connections, rate_limit_mbps, allow_tcp, allow_udp, description, sort_order, updated_at
FROM panel_token_defaults
ORDER BY sort_order ASC, plan_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]TokenPlanDefault, 0)
	for rows.Next() {
		plan, err := scanTokenPlanDefault(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	defaults := BuildTokenDefaults(plans)
	return &defaults, nil
}

func (s *MySQLStore) SaveTokenDefaults(ctx context.Context, input TokenDefaultsInput) (*TokenDefaults, error) {
	normalized, err := NormalizeTokenDefaults(input)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM panel_token_defaults`); err != nil {
		return nil, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO panel_token_defaults (
  plan_id, name, max_connections, rate_limit_mbps, allow_tcp, allow_udp, description, sort_order
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for _, plan := range normalized.Plans {
		if _, err := stmt.ExecContext(ctx,
			plan.PlanID, plan.Name, plan.MaxConnections, plan.RateLimitMbps,
			plan.AllowTCP, plan.AllowUDP, plan.Description, plan.SortOrder,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.GetTokenDefaults(ctx)
}

func (s *MySQLStore) SaveNodeReport(ctx context.Context, report NodeReportInput) (*NodeReport, error) {
	payload := report.Payload
	nodeID := strings.TrimSpace(payload.Node.ID)
	if nodeID == "" {
		return nil, errors.New("node.id is required")
	}
	reportedAt := payload.Timestamp
	if reportedAt.IsZero() {
		reportedAt = time.Now().UTC()
	}
	status := "online"
	if strings.TrimSpace(payload.Status) != "" && strings.TrimSpace(payload.Status) != "ok" {
		status = "error"
	}
	lastError := latestCommandError(payload.PanelCommands)
	metricsJSON, err := jsonString(payload.Metrics)
	if err != nil {
		return nil, fmt.Errorf("marshal metrics: %w", err)
	}
	commandsJSON, err := jsonString(payload.PanelCommands)
	if err != nil {
		return nil, fmt.Errorf("marshal panel commands: %w", err)
	}
	rawJSON := strings.TrimSpace(string(report.RawJSON))
	if rawJSON == "" {
		rawJSON, err = jsonString(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal raw report: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
UPDATE panel_nodes SET
  status = ?,
  current_version = ?,
  current_policy_revision = ?,
  last_report_at = ?,
  last_error = ?,
  updated_at = CURRENT_TIMESTAMP
WHERE node_id = ?`,
		status, strings.TrimSpace(payload.Version), strings.TrimSpace(payload.RoutePolicies.Revision), reportedAt, lastError, nodeID,
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}

	insertResult, err := tx.ExecContext(ctx, `
INSERT INTO panel_node_reports (
  node_id, version, status, route_policy_revision, route_policy_count,
  active_quic_connections, active_tcp_flows, active_udp_flows,
  metrics_json, panel_commands_json, raw_json, reported_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID, strings.TrimSpace(payload.Version), strings.TrimSpace(payload.Status), strings.TrimSpace(payload.RoutePolicies.Revision),
		payload.RoutePolicies.PolicyCount, payload.Metrics.ActiveQUICConnections, payload.Metrics.ActiveTCPFlows, payload.Metrics.ActiveUDPFlows,
		metricsJSON, commandsJSON, rawJSON, reportedAt,
	)
	if err != nil {
		return nil, err
	}
	reportID, err := insertResult.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := updateTasksFromCommandResults(ctx, tx, nodeID, payload.PanelCommands, reportedAt); err != nil {
		return nil, err
	}
	if err := updateNodePolicyRevisionFromReport(ctx, tx, nodeID, strings.TrimSpace(payload.RoutePolicies.Revision), lastError, reportedAt); err != nil {
		return nil, err
	}
	if err := saveClientSessionsFromReport(ctx, tx, nodeID, payload, reportedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getNodeReport(ctx, uint64(reportID))
}

func (s *MySQLStore) ListNodeReports(ctx context.Context, nodeID string, limit int) ([]NodeReport, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, node_id, version, status, route_policy_revision, route_policy_count,
  active_quic_connections, active_tcp_flows, active_udp_flows,
  metrics_json, panel_commands_json, raw_json, reported_at, created_at
FROM panel_node_reports
WHERE node_id = ?
ORDER BY reported_at DESC, id DESC
LIMIT ?`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := make([]NodeReport, 0)
	for rows.Next() {
		report, err := scanNodeReport(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reports, nil
}

func (s *MySQLStore) getNodeReport(ctx context.Context, id uint64) (*NodeReport, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, node_id, version, status, route_policy_revision, route_policy_count,
  active_quic_connections, active_tcp_flows, active_udp_flows,
  metrics_json, panel_commands_json, raw_json, reported_at, created_at
FROM panel_node_reports
WHERE id = ?`, id)
	report, err := scanNodeReport(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &report, nil
}

func (s *MySQLStore) CreateNodeTask(ctx context.Context, task NodeTaskInput) (*NodeTask, error) {
	task = normalizeTaskInput(task)
	if err := validateTaskInput(task); err != nil {
		return nil, err
	}
	payload, err := jsonString(task.RequestJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal task request: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO panel_node_tasks (
  task_id, node_id, type, status, priority, request_json
) VALUES (?, ?, ?, ?, ?, ?)`,
		task.TaskID, task.NodeID, task.Type, task.Status, task.Priority, payload,
	)
	if err != nil {
		return nil, err
	}
	return s.getNodeTask(ctx, task.TaskID)
}

func (s *MySQLStore) ListNodeTasks(ctx context.Context, nodeID string, limit int) ([]NodeTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, task_id, node_id, type, status, priority, request_json, result_json,
  error_message, queued_at, started_at, finished_at, created_at, updated_at
FROM panel_node_tasks
WHERE node_id = ?
ORDER BY queued_at DESC, id DESC
LIMIT ?`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]NodeTask, 0)
	for rows.Next() {
		task, err := scanNodeTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *MySQLStore) ClaimPendingNodeTasks(ctx context.Context, nodeID string, limit int, now time.Time) ([]NodeTask, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT id, task_id, node_id, type, status, priority, request_json, result_json,
  error_message, queued_at, started_at, finished_at, created_at, updated_at
FROM panel_node_tasks
WHERE node_id = ? AND status = ? AND type IN (?, ?, ?, ?, ?)
ORDER BY priority ASC, queued_at ASC, id ASC
LIMIT ?
FOR UPDATE`,
		nodeID, TaskStatusPending,
		panelcommand.CommandNoop, panelcommand.CommandConfigReload, panelcommand.CommandApplyConfig,
		panelcommand.CommandApplyPolicy, panelcommand.CommandStageUpgrade, limit,
	)
	if err != nil {
		return nil, err
	}
	tasks := make([]NodeTask, 0)
	taskIDs := make([]string, 0)
	for rows.Next() {
		task, err := scanNodeTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		task.Status = TaskStatusRunning
		task.StartedAt = &now
		tasks = append(tasks, task)
		taskIDs = append(taskIDs, task.TaskID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(taskIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(taskIDs)), ",")
		args := make([]any, 0, len(taskIDs)+3)
		args = append(args, TaskStatusRunning, now, nodeID)
		for _, taskID := range taskIDs {
			args = append(args, taskID)
		}
		query := fmt.Sprintf(`
UPDATE panel_node_tasks
SET status = ?, started_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE node_id = ? AND task_id IN (%s)`, placeholders)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *MySQLStore) getNodeTask(ctx context.Context, taskID string) (*NodeTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, task_id, node_id, type, status, priority, request_json, result_json,
  error_message, queued_at, started_at, finished_at, created_at, updated_at
FROM panel_node_tasks
WHERE task_id = ?`, taskID)
	task, err := scanNodeTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &task, nil
}

func (s *MySQLStore) GetNodeTask(ctx context.Context, taskID string) (*NodeTask, error) {
	return s.getNodeTask(ctx, taskID)
}

func (s *MySQLStore) UpdateNodeTask(ctx context.Context, update NodeTaskUpdate) (*NodeTask, error) {
	update.TaskID = strings.TrimSpace(update.TaskID)
	update.Status = strings.TrimSpace(update.Status)
	if update.TaskID == "" {
		return nil, errors.New("task_id is required")
	}
	if !isAllowedTaskStatus(update.Status) {
		return nil, fmt.Errorf("task status %q is not allowed", update.Status)
	}
	resultJSON, err := jsonString(update.ResultJSON)
	if err != nil {
		return nil, err
	}
	resultValue := any(resultJSON)
	if update.ResultJSON == nil {
		resultValue = nil
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE panel_node_tasks
SET status = ?, result_json = ?, error_message = ?,
  started_at = COALESCE(?, started_at),
  finished_at = COALESCE(?, finished_at),
  updated_at = CURRENT_TIMESTAMP
WHERE task_id = ?`,
		update.Status, resultValue, strings.TrimSpace(update.ErrorMessage),
		update.StartedAt, update.FinishedAt, update.TaskID,
	)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return nil, ErrNotFound
	}
	return s.getNodeTask(ctx, update.TaskID)
}

func (s *MySQLStore) AppendNodeTaskLog(ctx context.Context, input NodeTaskLogInput) (*NodeTaskLog, error) {
	input = normalizeTaskLogInput(input)
	if err := validateTaskLogInput(input); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO panel_node_task_logs (task_id, node_id, step, stream, message)
VALUES (?, ?, ?, ?, ?)`,
		input.TaskID, input.NodeID, input.Step, input.Stream, input.Message,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.getNodeTaskLog(ctx, uint64(id))
}

func (s *MySQLStore) ListNodeTaskLogs(ctx context.Context, taskID string, limit int) ([]NodeTaskLog, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, task_id, node_id, step, stream, message, created_at
FROM panel_node_task_logs
WHERE task_id = ?
ORDER BY id ASC
LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []NodeTaskLog
	for rows.Next() {
		log, err := scanNodeTaskLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func (s *MySQLStore) getNodeTaskLog(ctx context.Context, id uint64) (*NodeTaskLog, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, task_id, node_id, step, stream, message, created_at
FROM panel_node_task_logs
WHERE id = ?`, id)
	log, err := scanNodeTaskLog(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &log, nil
}

func (s *MySQLStore) UpsertNodeCredential(ctx context.Context, credential NodeCredentialInput) (*NodeCredential, error) {
	credential.NodeID = strings.TrimSpace(credential.NodeID)
	if _, err := s.GetNode(ctx, credential.NodeID); err != nil {
		return nil, err
	}
	if err := validateCredentialInput(credential); err != nil {
		return nil, err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO panel_node_credentials (
  node_id, auth_type, username, password_encrypted, private_key_encrypted,
  private_key_passphrase_encrypted, sudo_mode, is_one_time
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  auth_type = VALUES(auth_type),
  username = VALUES(username),
  password_encrypted = VALUES(password_encrypted),
  private_key_encrypted = VALUES(private_key_encrypted),
  private_key_passphrase_encrypted = VALUES(private_key_passphrase_encrypted),
  sudo_mode = VALUES(sudo_mode),
  is_one_time = VALUES(is_one_time),
  updated_at = CURRENT_TIMESTAMP`,
		credential.NodeID, credential.AuthType, credential.Username, nullIfEmpty(credential.PasswordEncrypted),
		nullIfEmpty(credential.PrivateKeyEncrypted), nullIfEmpty(credential.PrivateKeyPassphraseEncrypted),
		credential.SudoMode, credential.IsOneTime,
	)
	if err != nil {
		return nil, err
	}
	return s.GetNodeCredential(ctx, credential.NodeID)
}

func (s *MySQLStore) GetNodeCredential(ctx context.Context, nodeID string) (*NodeCredential, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, node_id, auth_type, username, password_encrypted, private_key_encrypted,
  private_key_passphrase_encrypted, sudo_mode, is_one_time, last_used_at, created_at, updated_at
FROM panel_node_credentials
WHERE node_id = ?`, strings.TrimSpace(nodeID))
	credential, err := scanNodeCredential(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &credential, nil
}

func (s *MySQLStore) DeleteNodeCredential(ctx context.Context, nodeID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM panel_node_credentials WHERE node_id = ?`, strings.TrimSpace(nodeID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQLStore) MarkNodeCredentialUsed(ctx context.Context, nodeID string, usedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE panel_node_credentials
SET last_used_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE node_id = ?`, usedAt, strings.TrimSpace(nodeID))
	return err
}

func (s *MySQLStore) UpdateNodeOperationalState(ctx context.Context, nodeID string, status string, currentVersion string, lastError string) error {
	nodeID = strings.TrimSpace(nodeID)
	status = strings.TrimSpace(status)
	if !nodeIDPattern.MatchString(nodeID) {
		return errors.New("node_id is invalid")
	}
	if status != "" && !isAllowedNodeStatus(status) {
		return fmt.Errorf("status %q is not allowed", status)
	}
	sets := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []any{}
	if status != "" {
		sets = append(sets, "status = ?")
		args = append(args, status)
	}
	if currentVersion != "" {
		sets = append(sets, "current_version = ?")
		args = append(args, strings.TrimSpace(currentVersion))
	}
	sets = append(sets, "last_error = ?")
	args = append(args, strings.TrimSpace(lastError))
	args = append(args, nodeID)
	res, err := s.db.ExecContext(ctx, "UPDATE panel_nodes SET "+strings.Join(sets, ", ")+" WHERE node_id = ?", args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQLStore) RecordAudit(ctx context.Context, entry AuditLog) error {
	payload, err := jsonString(entry.Request)
	if err != nil {
		return err
	}
	var operatorID any
	if entry.OperatorID != nil {
		operatorID = *entry.OperatorID
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO panel_audit_logs (
  operator_id, action, target_type, target_id, request_json, ip, user_agent
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		operatorID, entry.Action, entry.TargetType, entry.TargetID, payload, entry.IP, entry.UserAgent,
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(scanner rowScanner) (Node, error) {
	var node Node
	var tagsRaw sql.NullString
	var labelsRaw sql.NullString
	var hmacSecretEncrypted sql.NullString
	var hmacSecretSource sql.NullString
	var hmacSecretUpdatedAt sql.NullTime
	var lastReportAt sql.NullTime
	err := scanner.Scan(
		&node.ID, &node.NodeID, &node.Name, &node.Region, &node.Country, &node.Provider, &node.LineType,
		&node.EndpointHost, &node.EndpointPort, &node.ALPN, &node.AdminHost, &node.AdminPort,
		&node.SSHHost, &node.SSHPort, &node.SSHUser, &node.AllowTCP, &node.AllowUDP, &tagsRaw, &labelsRaw,
		&hmacSecretEncrypted, &hmacSecretSource, &hmacSecretUpdatedAt,
		&node.Status, &node.CurrentVersion, &node.DesiredVersion, &node.CurrentPolicyRevision,
		&node.DesiredPolicyRevision, &lastReportAt, &node.LastError, &node.CreatedAt, &node.UpdatedAt,
	)
	if err != nil {
		return Node{}, err
	}
	if tagsRaw.Valid && strings.TrimSpace(tagsRaw.String) != "" {
		if err := json.Unmarshal([]byte(tagsRaw.String), &node.Tags); err != nil {
			return Node{}, fmt.Errorf("decode node tags: %w", err)
		}
	}
	if node.Tags == nil {
		node.Tags = []string{}
	}
	if labelsRaw.Valid && strings.TrimSpace(labelsRaw.String) != "" {
		if err := json.Unmarshal([]byte(labelsRaw.String), &node.Labels); err != nil {
			return Node{}, fmt.Errorf("decode node labels: %w", err)
		}
	}
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	node.HMACSecretEncrypted = nullString(hmacSecretEncrypted)
	node.HMACSecretConfigured = strings.TrimSpace(node.HMACSecretEncrypted) != ""
	node.HMACSecretSource = nullString(hmacSecretSource)
	if hmacSecretUpdatedAt.Valid {
		node.HMACSecretUpdatedAt = &hmacSecretUpdatedAt.Time
	}
	if lastReportAt.Valid {
		node.LastReportAt = &lastReportAt.Time
	}
	return node, nil
}

func scanPanelUser(scanner rowScanner) (PanelUser, error) {
	var user PanelUser
	err := scanner.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func scanNodeReport(scanner rowScanner) (NodeReport, error) {
	var report NodeReport
	var metricsRaw sql.NullString
	var commandsRaw sql.NullString
	var raw sql.NullString
	err := scanner.Scan(
		&report.ID, &report.NodeID, &report.Version, &report.Status, &report.RoutePolicyRevision, &report.RoutePolicyCount,
		&report.ActiveQUICConnections, &report.ActiveTCPFlows, &report.ActiveUDPFlows,
		&metricsRaw, &commandsRaw, &raw, &report.ReportedAt, &report.CreatedAt,
	)
	if err != nil {
		return NodeReport{}, err
	}
	report.Metrics = nullStringJSON(metricsRaw)
	report.PanelCommands = nullStringJSON(commandsRaw)
	report.Raw = nullStringJSON(raw)
	return report, nil
}

func scanNodeTask(scanner rowScanner) (NodeTask, error) {
	var task NodeTask
	var requestRaw sql.NullString
	var resultRaw sql.NullString
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	err := scanner.Scan(
		&task.ID, &task.TaskID, &task.NodeID, &task.Type, &task.Status, &task.Priority,
		&requestRaw, &resultRaw, &task.ErrorMessage, &task.QueuedAt, &startedAt, &finishedAt, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return NodeTask{}, err
	}
	task.RequestJSON = nullStringJSON(requestRaw)
	task.ResultJSON = nullStringJSON(resultRaw)
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		task.FinishedAt = &finishedAt.Time
	}
	return task, nil
}

func scanNodeCredential(scanner rowScanner) (NodeCredential, error) {
	var credential NodeCredential
	var password sql.NullString
	var privateKey sql.NullString
	var passphrase sql.NullString
	var lastUsedAt sql.NullTime
	err := scanner.Scan(
		&credential.ID, &credential.NodeID, &credential.AuthType, &credential.Username,
		&password, &privateKey, &passphrase, &credential.SudoMode, &credential.IsOneTime,
		&lastUsedAt, &credential.CreatedAt, &credential.UpdatedAt,
	)
	if err != nil {
		return NodeCredential{}, err
	}
	credential.PasswordEncrypted = nullString(password)
	credential.PrivateKeyEncrypted = nullString(privateKey)
	credential.PrivateKeyPassphraseEncrypted = nullString(passphrase)
	credential.HasPassword = credential.PasswordEncrypted != ""
	credential.HasPrivateKey = credential.PrivateKeyEncrypted != ""
	credential.HasPrivatePassphrase = credential.PrivateKeyPassphraseEncrypted != ""
	if lastUsedAt.Valid {
		credential.LastUsedAt = &lastUsedAt.Time
	}
	return credential, nil
}

func scanNodeTaskLog(scanner rowScanner) (NodeTaskLog, error) {
	var log NodeTaskLog
	err := scanner.Scan(&log.ID, &log.TaskID, &log.NodeID, &log.Step, &log.Stream, &log.Message, &log.CreatedAt)
	return log, err
}

func scanPolicyRevision(scanner rowScanner) (PolicyRevision, error) {
	var policy PolicyRevision
	err := scanner.Scan(
		&policy.ID, &policy.Revision, &policy.SHA256, &policy.RoutePoliciesYAML, &policy.Source, &policy.CreatedAt,
	)
	return policy, err
}

func scanNodePolicyRevision(scanner rowScanner) (NodePolicyRevision, error) {
	var nodePolicy NodePolicyRevision
	var appliedAt sql.NullTime
	err := scanner.Scan(
		&nodePolicy.ID, &nodePolicy.NodeID, &nodePolicy.Revision, &nodePolicy.Desired, &nodePolicy.Applied,
		&appliedAt, &nodePolicy.LastError, &nodePolicy.CreatedAt, &nodePolicy.UpdatedAt,
	)
	if err != nil {
		return NodePolicyRevision{}, err
	}
	if appliedAt.Valid {
		nodePolicy.AppliedAt = &appliedAt.Time
	}
	return nodePolicy, nil
}

func scanTokenPlanDefault(scanner rowScanner) (TokenPlanDefault, error) {
	var plan TokenPlanDefault
	var updatedAt sql.NullTime
	err := scanner.Scan(
		&plan.PlanID, &plan.Name, &plan.MaxConnections, &plan.RateLimitMbps,
		&plan.AllowTCP, &plan.AllowUDP, &plan.Description, &plan.SortOrder, &updatedAt,
	)
	if err != nil {
		return TokenPlanDefault{}, err
	}
	if updatedAt.Valid {
		plan.UpdatedAt = &updatedAt.Time
	}
	return plan, nil
}

func updateTasksFromCommandResults(ctx context.Context, tx *sql.Tx, nodeID string, results []panelcommand.CommandResult, defaultTime time.Time) error {
	for _, result := range results {
		taskID := strings.TrimSpace(result.ID)
		if taskID == "" {
			continue
		}
		status := TaskStatusSuccess
		if !result.OK {
			status = TaskStatusFailed
		}
		executedAt := result.ExecutedAt
		if executedAt.IsZero() {
			executedAt = defaultTime
		}
		resultJSON, err := jsonString(result)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE panel_node_tasks
SET status = ?, result_json = ?, error_message = ?, finished_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE node_id = ? AND task_id = ?`,
			status, resultJSON, strings.TrimSpace(result.Error), executedAt, nodeID, taskID,
		); err != nil {
			return err
		}
	}
	return nil
}

func updateNodePolicyRevisionFromReport(ctx context.Context, tx *sql.Tx, nodeID string, revision string, lastError string, reportedAt time.Time) error {
	if revision != "" {
		if _, err := tx.ExecContext(ctx, `
UPDATE panel_node_policy_revisions
SET applied = 1, applied_at = ?, last_error = '', updated_at = CURRENT_TIMESTAMP
WHERE node_id = ? AND revision = ?`,
			reportedAt, nodeID, revision,
		); err != nil {
			return err
		}
	}
	if strings.TrimSpace(lastError) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
UPDATE panel_node_policy_revisions
SET last_error = ?, updated_at = CURRENT_TIMESTAMP
WHERE node_id = ? AND desired = 1 AND applied = 0`,
		strings.TrimSpace(lastError), nodeID,
	)
	return err
}

func normalizeTaskInput(task NodeTaskInput) NodeTaskInput {
	task.TaskID = strings.TrimSpace(task.TaskID)
	task.NodeID = strings.TrimSpace(task.NodeID)
	task.Type = strings.TrimSpace(task.Type)
	task.Status = strings.TrimSpace(task.Status)
	if task.Status == "" {
		task.Status = TaskStatusPending
	}
	if task.Priority <= 0 {
		task.Priority = 100
	}
	return task
}

func validateTaskInput(task NodeTaskInput) error {
	if task.TaskID == "" {
		return errors.New("task_id is required")
	}
	if !nodeIDPattern.MatchString(task.NodeID) {
		return errors.New("node_id is invalid")
	}
	if task.Type == "" {
		return errors.New("task type is required")
	}
	if !isAllowedTaskStatus(task.Status) {
		return fmt.Errorf("task status %q is not allowed", task.Status)
	}
	return nil
}

func isAllowedTaskStatus(status string) bool {
	switch status {
	case TaskStatusPending, TaskStatusRunning, TaskStatusSuccess, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func normalizeTaskLogInput(input NodeTaskLogInput) NodeTaskLogInput {
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.NodeID = strings.TrimSpace(input.NodeID)
	input.Step = strings.TrimSpace(input.Step)
	input.Stream = strings.TrimSpace(input.Stream)
	input.Message = strings.TrimSpace(input.Message)
	if input.Step == "" {
		input.Step = "task"
	}
	if input.Stream == "" {
		input.Stream = "info"
	}
	if len(input.Message) > 8000 {
		input.Message = input.Message[:8000]
	}
	return input
}

func validateTaskLogInput(input NodeTaskLogInput) error {
	if input.TaskID == "" {
		return errors.New("task_id is required")
	}
	if !nodeIDPattern.MatchString(input.NodeID) {
		return errors.New("node_id is invalid")
	}
	switch input.Stream {
	case "info", "stdout", "stderr", "error":
		return nil
	default:
		return fmt.Errorf("task log stream %q is not allowed", input.Stream)
	}
}

func validateCredentialInput(credential NodeCredentialInput) error {
	if !nodeIDPattern.MatchString(credential.NodeID) {
		return errors.New("node_id is invalid")
	}
	if credential.AuthType != CredentialAuthPassword && credential.AuthType != CredentialAuthPrivateKey {
		return errors.New("credential auth_type is invalid")
	}
	if credential.Username == "" {
		return errors.New("credential username is required")
	}
	if credential.SudoMode != CredentialSudoRoot && credential.SudoMode != CredentialSudoSudo {
		return errors.New("credential sudo_mode is invalid")
	}
	if credential.AuthType == CredentialAuthPassword && credential.PasswordEncrypted == "" {
		return errors.New("credential password is required")
	}
	if credential.AuthType == CredentialAuthPrivateKey && credential.PrivateKeyEncrypted == "" {
		return errors.New("credential private_key is required")
	}
	return nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullStringJSON(value sql.NullString) json.RawMessage {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return json.RawMessage(value.String)
}

func jsonString(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
