-- gaccel 业务后台节点表与添加节点模板。
-- 这个 SQL 给业务后台自己的数据库使用，不是给 gaccel-panel 控制面板数据库使用。
-- hmac_secret 只能保存在服务端，不能返回给客户端。

CREATE TABLE IF NOT EXISTS accel_nodes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL COMMENT '节点稳定业务 ID，业务后台、控制面板、节点配置三方保持一致',
  name VARCHAR(128) NOT NULL COMMENT '节点展示名称',
  region VARCHAR(64) NOT NULL DEFAULT '' COMMENT '区域代码，例如 hk、jp、us',
  country VARCHAR(32) NOT NULL DEFAULT '' COMMENT '国家或地区代码，例如 HK、JP',
  provider VARCHAR(64) NOT NULL DEFAULT '' COMMENT '云厂商或线路供应商',
  line_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '线路类型，例如 premium、standard',
  endpoint_host VARCHAR(255) NOT NULL COMMENT '客户端连接节点 QUIC 的公网 IP 或域名',
  endpoint_port INT UNSIGNED NOT NULL COMMENT '客户端连接节点 QUIC 的 UDP 端口',
  alpn VARCHAR(64) NOT NULL DEFAULT 'gaccel/1' COMMENT 'QUIC ALPN，当前默认 gaccel/1',
  admin_host VARCHAR(255) NOT NULL DEFAULT '127.0.0.1' COMMENT '节点 admin API 地址，控制面板诊断使用，不给客户端使用',
  admin_port INT UNSIGNED NOT NULL DEFAULT 5557 COMMENT '节点 admin API 端口',
  ssh_host VARCHAR(255) NOT NULL DEFAULT '' COMMENT '控制面板一键部署/更新使用的 SSH 地址',
  ssh_port INT UNSIGNED NOT NULL DEFAULT 22 COMMENT 'SSH 端口',
  ssh_user VARCHAR(64) NOT NULL DEFAULT 'root' COMMENT 'SSH 用户',
  hmac_secret VARCHAR(128) NOT NULL COMMENT '每个节点独立的 JWT HMAC 签名密钥，业务后台保存明文，禁止下发客户端',
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否允许 TCP flow',
  allow_udp TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否允许 UDP flow',
  status ENUM('new','deploying','online','offline','error','disabled') NOT NULL DEFAULT 'new',
  tags JSON NULL COMMENT '节点标签数组，例如 ["steam","quic"]',
  labels JSON NULL COMMENT '节点扩展字段对象，例如 {"line":"premium"}',
  desired_version VARCHAR(64) NOT NULL DEFAULT '' COMMENT '期望节点版本，空字符串表示不指定',
  desired_policy_revision VARCHAR(64) NOT NULL DEFAULT '' COMMENT '期望策略版本，空字符串表示不指定',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_node_id (node_id),
  KEY idx_region (region),
  KEY idx_status (status),
  KEY idx_desired_policy (desired_policy_revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='游戏加速节点';

-- 执行前只需要修改这一段变量。
SET @node_id = 'hk-01';
SET @name = '香港 01';
SET @region = 'hk';
SET @country = 'HK';
SET @provider = 'aliyun';
SET @line_type = 'premium';
SET @endpoint_host = '47.83.160.126';
SET @endpoint_port = 6666;
SET @alpn = 'gaccel/1';
SET @admin_host = '47.83.160.126';
SET @admin_port = 5557;
SET @ssh_host = '47.83.160.126';
SET @ssh_port = 22;
SET @ssh_user = 'root';

-- 每个节点生成一次独立密钥。
-- 业务后台要保存这个明文，用于签发客户端 JWT。
-- 不要把这个值给客户端，也不要写到业务日志里。
SET @hmac_secret = LOWER(HEX(RANDOM_BYTES(32)));

INSERT INTO accel_nodes (
  node_id, name, region, country, provider, line_type,
  endpoint_host, endpoint_port, alpn,
  admin_host, admin_port,
  ssh_host, ssh_port, ssh_user,
  hmac_secret, allow_tcp, allow_udp,
  status, tags, labels,
  desired_version, desired_policy_revision
) VALUES (
  @node_id, @name, @region, @country, @provider, @line_type,
  @endpoint_host, @endpoint_port, @alpn,
  @admin_host, @admin_port,
  @ssh_host, @ssh_port, @ssh_user,
  @hmac_secret, 1, 1,
  'new', JSON_ARRAY('quic'), JSON_OBJECT('line', @line_type),
  '', ''
)
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
  status = VALUES(status),
  tags = VALUES(tags),
  labels = VALUES(labels),
  updated_at = CURRENT_TIMESTAMP;

-- 仅首次新增节点时复制这个结果。
-- 已有节点更新 IP、端口、线路时，不要覆盖旧 hmac_secret，否则旧 token 会全部失效。
SELECT @node_id AS 节点ID, @hmac_secret AS 本次生成的节点HMAC密钥;
