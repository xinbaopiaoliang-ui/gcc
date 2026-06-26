export type NodeStatus = "new" | "deploying" | "online" | "offline" | "error" | "disabled";

export type PanelUserRole = "admin" | "operator" | "viewer";
export type PanelUserStatus = "active" | "disabled";

export interface PanelUser {
  id: number;
  username: string;
  role: PanelUserRole;
  status: PanelUserStatus;
  created_at: string;
  updated_at: string;
}

export interface LoginInput {
  username: string;
  password: string;
}

export interface LoginResponse {
  status: string;
  token: string;
  access_token: string;
  token_type: string;
  expires_at: string;
  expires_in_seconds: number;
  user: PanelUser;
}

export interface MeResponse {
  user: PanelUser;
}

export interface ChangePasswordInput {
  current_password: string;
  new_password: string;
}

export interface ChangePasswordResponse {
  status: string;
  token: string;
  access_token: string;
  token_type: string;
  expires_at: string;
  expires_in_seconds: number;
  user: PanelUser;
}

export interface PanelUserListResponse {
  users: PanelUser[];
  count: number;
}

export interface PanelUserResponse {
  user: PanelUser;
}

export interface PanelUserCreateInput {
  username: string;
  password: string;
  role?: PanelUserRole;
  status?: PanelUserStatus;
}

export interface PanelUserUpdateInput {
  role: PanelUserRole;
  status: PanelUserStatus;
}

export interface PanelUserPasswordResetInput {
  new_password: string;
}

export interface PanelNode {
  id: number;
  node_id: string;
  name: string;
  region: string;
  country: string;
  provider: string;
  line_type: string;
  endpoint_host: string;
  endpoint_port: number;
  alpn: string;
  admin_host: string;
  admin_port: number;
  ssh_host: string;
  ssh_port: number;
  ssh_user: string;
  allow_tcp: boolean;
  allow_udp: boolean;
  hmac_secret_configured: boolean;
  hmac_secret_source?: string;
  hmac_secret_updated_at?: string;
  tags: string[];
  labels: Record<string, string>;
  status: NodeStatus;
  current_version: string;
  desired_version: string;
  current_policy_revision: string;
  desired_policy_revision: string;
  last_report_at?: string;
  last_error: string;
  created_at: string;
  updated_at: string;
}

export interface NodeInput {
  node_id: string;
  name: string;
  region?: string;
  country?: string;
  provider?: string;
  line_type?: string;
  endpoint_host: string;
  endpoint_port: number;
  alpn?: string;
  admin_host?: string;
  admin_port?: number;
  ssh_host?: string;
  ssh_port?: number;
  ssh_user?: string;
  allow_tcp?: boolean;
  allow_udp?: boolean;
  hmac_secret?: string;
  tags?: string[];
  labels?: Record<string, string>;
  status?: NodeStatus;
  desired_version?: string;
  desired_policy_revision?: string;
}

export interface NodeListResponse {
  nodes: PanelNode[];
  count: number;
}

export interface NodeResponse {
  node: PanelNode;
}

export type NodeTaskStatus = "pending" | "running" | "success" | "failed" | "cancelled";

export interface NodeReport {
  id: number;
  node_id: string;
  version: string;
  status: string;
  route_policy_revision: string;
  route_policy_count: number;
  active_quic_connections: number;
  active_tcp_flows: number;
  active_udp_flows: number;
  metrics_json?: unknown;
  panel_commands_json?: unknown;
  raw_json?: unknown;
  reported_at: string;
  created_at: string;
}

export interface NodeReportsResponse {
  reports: NodeReport[];
  count: number;
}

export interface NodeTask {
  id: number;
  task_id: string;
  node_id: string;
  type: string;
  status: NodeTaskStatus;
  priority: number;
  request_json?: unknown;
  result_json?: unknown;
  error_message: string;
  queued_at: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
}

export interface NodeTasksResponse {
  tasks: NodeTask[];
  count: number;
}

export interface NodeTaskResponse {
  task: NodeTask;
}

export interface RetryTaskResponse {
  task: NodeTask;
  source_task: NodeTask;
}

export interface NodeTaskLog {
  id: number;
  task_id: string;
  node_id: string;
  step: string;
  stream: "info" | "stdout" | "stderr" | "error";
  message: string;
  created_at: string;
}

export interface NodeTaskLogsResponse {
  logs: NodeTaskLog[];
  count: number;
}

export interface DeployNodeInput {
  version?: string;
  hmac_secret?: string;
  panel_base_url?: string;
}

export interface UpdateNodeInput {
  version?: string;
}

export interface RepairAdminInput {
  listen_host?: string;
}

export interface TuneUDPBufferInput {
  receive_buffer_bytes?: number;
  send_buffer_bytes?: number;
}

export interface ApplyPolicyInput {
  revision?: string;
  sha256?: string;
  route_policies_yaml: string;
}

export interface PolicyRevision {
  id: number;
  revision: string;
  sha256: string;
  route_policies_yaml: string;
  source: "backend" | "manual";
  created_at: string;
}

export interface PolicyRevisionListResponse {
  policy_revisions: PolicyRevision[];
  count: number;
}

export interface PolicyRevisionResponse {
  policy_revision: PolicyRevision;
}

export interface PolicyRevisionInput {
  revision: string;
  sha256?: string;
  route_policies_yaml: string;
}

export interface PolicyValidationInput {
  revision: string;
  sha256?: string;
  route_policies_yaml: string;
  base_revision?: string;
  base_route_policies_yaml?: string;
}

export interface PolicyValidationSummary {
  revision: string;
  policy_count: number;
  rule_count: number;
  relay_rule_count: number;
  disabled_rules: number;
  games: string[];
  policies: string[];
  networks: string[];
  target_types: string[];
}

export interface PolicyValidationDiff {
  base_revision: string;
  candidate_revision: string;
  added_policies: string[];
  removed_policies: string[];
  changed_policies: string[];
  added_rules: string[];
  removed_rules: string[];
  changed_rules: string[];
  line_diff: string[];
}

export interface PolicyValidationResponse {
  valid: boolean;
  sha256: string;
  errors: string[];
  warnings: string[];
  summary: PolicyValidationSummary;
  diff?: PolicyValidationDiff;
}

export interface NodeSyncStatus {
  node_id: string;
  status: string;
  current_version: string;
  desired_version: string;
  version_state: "unknown" | "not_set" | "synced" | "waiting_report" | "pending" | string;
  current_policy_revision: string;
  desired_policy_revision: string;
  policy_state: "unknown" | "not_set" | "synced" | "waiting_report" | "pending" | string;
  last_report_at?: string;
  report_age_seconds?: number;
  last_error: string;
  pending_tasks: number;
  running_tasks: number;
  failed_tasks: number;
  latest_task?: NodeTask;
  hmac_secret_configured: boolean;
  hmac_secret_source?: string;
  hmac_secret_updated_at?: string;
  deploy_ready: boolean;
  recommendations: string[];
}

export interface NodeSyncStatusResponse {
  sync_status: NodeSyncStatus;
}

export type DiagnosticStatus = "ok" | "warning" | "error" | string;

export interface DiagnosticCheck {
  key: string;
  label: string;
  status: DiagnosticStatus;
  message: string;
  detail?: Record<string, unknown>;
}

export interface DiagnosticSummary {
  ok: number;
  warning: number;
  error: number;
  total: number;
}

export interface SystemCheckResponse {
  status: DiagnosticStatus;
  version: string;
  generated_at: string;
  config: {
    listen: string;
    public_base_url?: string;
    database_driver: string;
    web_root?: string;
    backend_api_key_count: number;
    session_ttl_seconds: number;
    cors_allowed_origins: string[];
    default_node_version: string;
    ssh_timeout_seconds: number;
    command_timeout_seconds: number;
  };
  summary: DiagnosticSummary;
  checks: DiagnosticCheck[];
}

export interface TokenPlanDefault {
  plan_id: string;
  name: string;
  max_connections: number;
  rate_limit_mbps: number;
  allow_tcp: boolean;
  allow_udp: boolean;
  description?: string;
  sort_order: number;
  updated_at?: string;
}

export interface TokenDefaults {
  node_hard_limit: number;
  plans: TokenPlanDefault[];
  updated_at?: string;
}

export interface TokenDefaultsInput {
  plans: TokenPlanDefault[];
}

export interface TokenDefaultsResponse {
  token_defaults: TokenDefaults;
}

export interface TrafficByteBreakdown {
  tcp_client_to_target_bytes: number;
  tcp_target_to_client_bytes: number;
  udp_client_to_target_bytes: number;
  udp_target_to_client_bytes: number;
  total_bytes: number;
}

export interface TrafficTotals {
  node_count: number;
  online_node_count: number;
  report_node_count: number;
  active_quic_connections: number;
  active_tcp_flows: number;
  active_udp_flows: number;
  tcp_client_to_target_bytes: number;
  tcp_target_to_client_bytes: number;
  udp_client_to_target_bytes: number;
  udp_target_to_client_bytes: number;
  total_bytes: number;
  flow_open_errors: number;
  flow_close_events: number;
  udp_packet_drops: number;
  policy_drift_nodes: number;
}

export interface TrafficNodeStats {
  node_id: string;
  name: string;
  region: string;
  endpoint: string;
  status: string;
  current_version: string;
  desired_version: string;
  current_policy_revision: string;
  desired_policy_revision: string;
  policy_state: string;
  report_count: number;
  latest_report_at?: string;
  report_age_seconds: number;
  active_quic_connections: number;
  active_tcp_flows: number;
  active_udp_flows: number;
  traffic: TrafficByteBreakdown;
  last_error: string;
  labels?: Record<string, string>;
  tags?: string[];
}

export interface TrafficUserStats {
  user_id: string;
  active_connections: number;
  traffic: TrafficByteBreakdown;
  node_count: number;
}

export interface TrafficFlowEventStats {
  network: string;
  event: string;
  reason: string;
  game_id?: string;
  policy_id?: string;
  count: number;
}

export interface TrafficPolicyEventStats {
  game_id?: string;
  policy_id?: string;
  network: string;
  open: number;
  close: number;
  error: number;
  total: number;
}

export interface TrafficPolicyConsistency {
  node_id: string;
  name: string;
  current_policy_revision: string;
  desired_policy_revision: string;
  state: string;
  last_report_at?: string;
  report_age_seconds: number;
  last_error: string;
}

export interface TrafficOverview {
  window_seconds: number;
  window_started_at: string;
  generated_at: string;
  sample_mode: "window_delta" | "latest_cumulative" | string;
  totals: TrafficTotals;
  nodes: TrafficNodeStats[];
  users: TrafficUserStats[];
  flow_events: TrafficFlowEventStats[];
  policy_events: TrafficPolicyEventStats[];
  policy_consistency: TrafficPolicyConsistency[];
  recommendations: string[];
}

export interface TrafficOverviewResponse {
  status: string;
  traffic: TrafficOverview;
  overview: TrafficOverview;
}

export type ClientSessionStatus = "online" | "closed" | string;

export interface ClientSessionOverview {
  online_sessions: number;
  closed_sessions: number;
  timeout_sessions: number;
  total_sessions: number;
  total_duration_seconds: number;
  udp_client_to_target_bytes: number;
  udp_target_to_client_bytes: number;
  tcp_client_to_target_bytes: number;
  tcp_target_to_client_bytes: number;
}

export interface ClientSession {
  id: number;
  node_id: string;
  session_id: string;
  remote_addr: string;
  user_id: string;
  device_id: string;
  client_id: string;
  client_version: string;
  client_platform: string;
  protocol_version: number;
  status: ClientSessionStatus;
  close_reason: string;
  close_source: string;
  game_ids: string[];
  policy_ids: string[];
  config_revision: string;
  connected_at: string;
  authenticated_at?: string;
  last_seen_at: string;
  last_ping_at?: string;
  ended_at?: string;
  duration_seconds: number;
  max_connections: number;
  rate_limit_mbps: number;
  allow_tcp: boolean;
  allow_udp: boolean;
  udp_flows: number;
  tcp_flows: number;
  udp_client_to_target_bytes: number;
  udp_target_to_client_bytes: number;
  tcp_client_to_target_bytes: number;
  tcp_target_to_client_bytes: number;
  created_at: string;
  updated_at: string;
}

export interface ClientSessionListResponse {
  status: string;
  sessions: ClientSession[];
  overview: ClientSessionOverview;
  limit: number;
  offset: number;
  count: number;
}

export interface NodeDiagnosticsResponse {
  status: DiagnosticStatus;
  node_id: string;
  generated_at: string;
  admin_url: string;
  sync_status: NodeSyncStatus;
  summary: DiagnosticSummary;
  checks: DiagnosticCheck[];
  recommendations: string[];
}

export interface NodeNetworkDiagnosticsMetrics {
  receive_buffer_max: number;
  send_buffer_max: number;
  receive_buffer_default: number;
  send_buffer_default: number;
  netdev_max_backlog: number;
  udp_socket_count: number;
  udp_recv_queue_total: number;
  udp_send_queue_total: number;
  rx_dropped: number;
  tx_dropped: number;
  rx_errors: number;
  tx_errors: number;
  udp_buffer_warnings: number;
  node_warn_or_error_logs: number;
  accepted_connections: number;
  authenticated_connections: number;
  cpu_count: number;
  load_average: string;
}

export interface NodeNetworkDiagnosticsResponse {
  status: DiagnosticStatus;
  node_id: string;
  generated_at: string;
  risk_level: "low" | "medium" | "high" | string;
  risk_score: number;
  summary: DiagnosticSummary;
  metrics: NodeNetworkDiagnosticsMetrics;
  checks: DiagnosticCheck[];
  recommendations: string[];
  raw?: Record<string, string>;
}

export interface NodeConnectivityProbeMetrics {
  resolved_ips: string[];
  dns_latency_ms: number;
  admin_tcp_latency_ms: number;
  admin_health_latency_ms: number;
  admin_http_status: number;
  quic_handshake_latency_ms: number;
  quic_auth_ping_latency_ms: number;
  server_alpn: string;
  protocol_version: number;
  token_policy: string;
  capabilities: string[];
}

export interface NodeConnectivityProbeResponse {
  status: DiagnosticStatus;
  node_id: string;
  generated_at: string;
  endpoint: string;
  admin_url: string;
  summary: DiagnosticSummary;
  metrics: NodeConnectivityProbeMetrics;
  checks: DiagnosticCheck[];
  recommendations: string[];
}

export interface DesiredPolicyInput {
  revision: string;
  create_task?: boolean;
  priority?: number;
}

export interface NodePolicyRevision {
  id: number;
  node_id: string;
  revision: string;
  desired: boolean;
  applied: boolean;
  applied_at?: string;
  last_error: string;
  created_at: string;
  updated_at: string;
}

export interface DesiredPolicyResponse {
  policy_revision: PolicyRevision;
  node_policy: NodePolicyRevision;
  task?: NodeTask | null;
}

export interface NodeCredential {
  id: number;
  node_id: string;
  auth_type: "password" | "private_key";
  username: string;
  sudo_mode: "root" | "sudo";
  is_one_time: boolean;
  has_password: boolean;
  has_private_key: boolean;
  has_private_key_passphrase: boolean;
  last_used_at?: string;
  created_at: string;
  updated_at: string;
}

export interface NodeCredentialInput {
  auth_type: "password" | "private_key";
  username: string;
  password?: string;
  private_key?: string;
  private_key_passphrase?: string;
  sudo_mode: "root" | "sudo";
  is_one_time: boolean;
}

export interface NodeCredentialResponse {
  credential: NodeCredential | null;
}

export interface SSHCredentialTestResult {
  ok: boolean;
  node_id: string;
  address: string;
  latency_ms: number;
  output?: string;
  error?: string;
}

export interface SSHCredentialTestResponse {
  result: SSHCredentialTestResult;
}

export interface SecurityOverview {
  users: {
    total: number;
    admins: number;
    active: number;
    disabled: number;
  };
  nodes: {
    total: number;
    with_credentials: number;
    without_credentials: number;
    without_hmac_secret: number;
    disabled: number;
    offline_or_error: number;
    policy_drift: number;
    version_drift: number;
  };
  config: {
    listen: string;
    public_base_url?: string;
    session_ttl_seconds: number;
    backend_api_key_count: number;
    master_key_configured: boolean;
    session_secret_configured: boolean;
    command_secret_configured: boolean;
    cors_allowed_origins: string[];
  };
  warnings: string[];
}

export interface SecurityOverviewResponse {
  security: SecurityOverview;
}

export interface APIErrorBody {
  error?: {
    code?: string;
    message?: string;
  };
}
