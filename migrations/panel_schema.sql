CREATE TABLE IF NOT EXISTS panel_users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role ENUM('admin','operator','viewer') NOT NULL DEFAULT 'operator',
  status ENUM('active','disabled') NOT NULL DEFAULT 'active',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_nodes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL,
  name VARCHAR(128) NOT NULL,
  region VARCHAR(64) NOT NULL DEFAULT '',
  country VARCHAR(32) NOT NULL DEFAULT '',
  provider VARCHAR(64) NOT NULL DEFAULT '',
  line_type VARCHAR(64) NOT NULL DEFAULT '',
  endpoint_host VARCHAR(255) NOT NULL,
  endpoint_port INT UNSIGNED NOT NULL,
  alpn VARCHAR(64) NOT NULL DEFAULT 'gaccel/1',
  admin_host VARCHAR(255) NOT NULL DEFAULT '127.0.0.1',
  admin_port INT UNSIGNED NOT NULL DEFAULT 5557,
  ssh_host VARCHAR(255) NOT NULL,
  ssh_port INT UNSIGNED NOT NULL DEFAULT 22,
  ssh_user VARCHAR(64) NOT NULL DEFAULT 'root',
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1,
  allow_udp TINYINT(1) NOT NULL DEFAULT 1,
  hmac_secret_encrypted TEXT NULL,
  hmac_secret_source VARCHAR(32) NOT NULL DEFAULT '',
  hmac_secret_updated_at DATETIME NULL,
  tags JSON NULL,
  labels JSON NULL,
  status ENUM('new','deploying','online','offline','error','disabled') NOT NULL DEFAULT 'new',
  current_version VARCHAR(64) NOT NULL DEFAULT '',
  desired_version VARCHAR(64) NOT NULL DEFAULT '',
  current_policy_revision VARCHAR(64) NOT NULL DEFAULT '',
  desired_policy_revision VARCHAR(64) NOT NULL DEFAULT '',
  last_report_at DATETIME NULL,
  last_error VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_node_id (node_id),
  KEY idx_status (status),
  KEY idx_region (region),
  KEY idx_policy_revision (current_policy_revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_node_credentials (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL,
  auth_type ENUM('password','private_key') NOT NULL DEFAULT 'password',
  username VARCHAR(64) NOT NULL DEFAULT 'root',
  password_encrypted TEXT NULL,
  private_key_encrypted TEXT NULL,
  private_key_passphrase_encrypted TEXT NULL,
  sudo_mode ENUM('root','sudo') NOT NULL DEFAULT 'root',
  is_one_time TINYINT(1) NOT NULL DEFAULT 0,
  last_used_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_node_credential (node_id),
  KEY idx_auth_type (auth_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_node_reports (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL,
  version VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT '',
  route_policy_revision VARCHAR(64) NOT NULL DEFAULT '',
  route_policy_count INT UNSIGNED NOT NULL DEFAULT 0,
  active_quic_connections BIGINT NOT NULL DEFAULT 0,
  active_tcp_flows BIGINT NOT NULL DEFAULT 0,
  active_udp_flows BIGINT NOT NULL DEFAULT 0,
  metrics_json JSON NULL,
  panel_commands_json JSON NULL,
  raw_json JSON NOT NULL,
  reported_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_node_reported_at (node_id, reported_at),
  KEY idx_policy_revision (route_policy_revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_client_sessions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL,
  session_id VARCHAR(128) NOT NULL,
  remote_addr VARCHAR(128) NOT NULL DEFAULT '',
  user_id VARCHAR(128) NOT NULL DEFAULT '',
  device_id VARCHAR(128) NOT NULL DEFAULT '',
  client_id VARCHAR(128) NOT NULL DEFAULT '',
  client_version VARCHAR(64) NOT NULL DEFAULT '',
  client_platform VARCHAR(64) NOT NULL DEFAULT '',
  protocol_version INT UNSIGNED NOT NULL DEFAULT 0,
  status ENUM('online','closed') NOT NULL DEFAULT 'online',
  close_reason VARCHAR(64) NOT NULL DEFAULT '',
  close_source VARCHAR(32) NOT NULL DEFAULT '',
  game_ids JSON NULL,
  policy_ids JSON NULL,
  config_revision VARCHAR(64) NOT NULL DEFAULT '',
  connected_at DATETIME NOT NULL,
  authenticated_at DATETIME NULL,
  last_seen_at DATETIME NOT NULL,
  last_ping_at DATETIME NULL,
  ended_at DATETIME NULL,
  duration_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
  max_connections INT UNSIGNED NOT NULL DEFAULT 0,
  rate_limit_mbps INT UNSIGNED NOT NULL DEFAULT 0,
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1,
  allow_udp TINYINT(1) NOT NULL DEFAULT 1,
  udp_flows INT UNSIGNED NOT NULL DEFAULT 0,
  tcp_flows INT UNSIGNED NOT NULL DEFAULT 0,
  udp_client_to_target_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  udp_target_to_client_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  tcp_client_to_target_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  tcp_target_to_client_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_node_session (node_id, session_id),
  KEY idx_user_time (user_id, connected_at),
  KEY idx_device_time (device_id, connected_at),
  KEY idx_node_status_time (node_id, status, last_seen_at),
  KEY idx_close_reason (close_reason, ended_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_client_session_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL,
  session_id VARCHAR(128) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  user_id VARCHAR(128) NOT NULL DEFAULT '',
  device_id VARCHAR(128) NOT NULL DEFAULT '',
  close_reason VARCHAR(64) NOT NULL DEFAULT '',
  close_source VARCHAR(32) NOT NULL DEFAULT '',
  occurred_at DATETIME NOT NULL,
  payload_json JSON NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_node_session_event (node_id, session_id, occurred_at),
  KEY idx_user_event_time (user_id, occurred_at),
  KEY idx_event_type_time (event_type, occurred_at),
  KEY idx_close_reason_time (close_reason, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_node_tasks (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  task_id VARCHAR(96) NOT NULL,
  node_id VARCHAR(96) NOT NULL,
  type ENUM('deploy_node','update_node','restart_node','health_check','apply_config','apply_policy','stage_upgrade') NOT NULL,
  status ENUM('pending','running','success','failed','cancelled') NOT NULL DEFAULT 'pending',
  priority INT NOT NULL DEFAULT 100,
  request_json JSON NULL,
  result_json JSON NULL,
  error_message VARCHAR(1024) NOT NULL DEFAULT '',
  operator_id BIGINT UNSIGNED NULL,
  queued_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at DATETIME NULL,
  finished_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_task_id (task_id),
  KEY idx_node_status (node_id, status),
  KEY idx_status_priority (status, priority, queued_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_node_task_logs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  task_id VARCHAR(96) NOT NULL,
  node_id VARCHAR(96) NOT NULL,
  step VARCHAR(96) NOT NULL,
  stream ENUM('info','stdout','stderr','error') NOT NULL DEFAULT 'info',
  message TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_task_created (task_id, created_at),
  KEY idx_node_created (node_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_policy_revisions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  revision VARCHAR(64) NOT NULL,
  sha256 CHAR(64) NOT NULL,
  route_policies_yaml MEDIUMTEXT NOT NULL,
  source ENUM('backend','manual') NOT NULL DEFAULT 'backend',
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_revision (revision),
  KEY idx_sha256 (sha256)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_node_policy_revisions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL,
  revision VARCHAR(64) NOT NULL,
  desired TINYINT(1) NOT NULL DEFAULT 1,
  applied TINYINT(1) NOT NULL DEFAULT 0,
  applied_at DATETIME NULL,
  last_error VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_node_revision (node_id, revision),
  KEY idx_revision (revision),
  KEY idx_node_desired (node_id, desired)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS panel_token_defaults (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  plan_id VARCHAR(64) NOT NULL,
  name VARCHAR(64) NOT NULL,
  max_connections INT UNSIGNED NOT NULL,
  rate_limit_mbps INT UNSIGNED NOT NULL,
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1,
  allow_udp TINYINT(1) NOT NULL DEFAULT 1,
  description VARCHAR(255) NOT NULL DEFAULT '',
  sort_order INT NOT NULL DEFAULT 100,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plan_id (plan_id),
  KEY idx_sort_order (sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO panel_token_defaults
  (plan_id, name, max_connections, rate_limit_mbps, allow_tcp, allow_udp, description, sort_order)
VALUES
  ('trial', '免费/测试', 32, 50, 1, 1, '短时测试、体验用户和低并发调试。', 10),
  ('standard', '普通', 64, 100, 1, 1, '默认游戏加速档位，适合 Steam 商店、社区和常规在线游戏。', 20),
  ('advanced', '高级', 128, 200, 1, 1, '推荐给 Steam 客户端联调和多连接游戏场景。', 30),
  ('premium', '旗舰', 256, 500, 1, 1, '高并发、多游戏下载和重度游戏加速档位。', 40);

CREATE TABLE IF NOT EXISTS panel_audit_logs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  operator_id BIGINT UNSIGNED NULL,
  action VARCHAR(96) NOT NULL,
  target_type VARCHAR(64) NOT NULL,
  target_id VARCHAR(128) NOT NULL,
  request_json JSON NULL,
  ip VARCHAR(64) NOT NULL DEFAULT '',
  user_agent VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_operator_created (operator_id, created_at),
  KEY idx_target_created (target_type, target_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
