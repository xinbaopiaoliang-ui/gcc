# 业务后台节点 MySQL 与控制面板同步说明

适用版本：gaccel panel v0.6.x

这份文档给业务后台使用。核心原则是：业务后台保存自己的节点主数据和节点 `hmac_secret` 明文，控制面板保存节点资料和 `hmac_secret` 的加密副本，客户端永远不能拿到 `hmac_secret`。

## 一、职责边界

| 系统 | 负责什么 | 不负责什么 |
| --- | --- | --- |
| 业务后台 | 用户、套餐、订单、游戏配置、节点主数据、节点 `hmac_secret`、客户端 token 签发 | 不直接参与 QUIC 转发 |
| 控制面板 | 节点展示、部署、更新、SSH 凭据、策略下发、节点上报、流量观测 | 不给客户端签发业务 token，不做用户套餐判断 |
| 节点 | 验证客户端 token，按本地策略转发 TCP/UDP | 不知道业务订单，不识别本地进程 |
| 客户端 | 从业务后台拿配置，按进程和规则分流，直接连接节点 QUIC | 不访问控制面板 API，不保存 `hmac_secret` |

节点三方统一使用 `node_id` 关联，不要用 IP 当主键。IP、端口、线路都可以变，`node_id` 要稳定。

## 二、业务后台建议新增节点表

下面 SQL 是给业务后台自己的 MySQL 库用的，不是控制面板库。

```sql
CREATE TABLE IF NOT EXISTS accel_nodes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(96) NOT NULL COMMENT '节点稳定业务ID，和控制面板、节点配置保持一致',
  name VARCHAR(128) NOT NULL COMMENT '节点展示名称',
  region VARCHAR(64) NOT NULL DEFAULT '' COMMENT '区域，例如 hk、jp、us',
  country VARCHAR(32) NOT NULL DEFAULT '' COMMENT '国家/地区，例如 HK、JP',
  provider VARCHAR(64) NOT NULL DEFAULT '' COMMENT '服务商',
  line_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '线路类型，例如 premium、standard',
  endpoint_host VARCHAR(255) NOT NULL COMMENT '客户端连接 QUIC 的公网 IP 或域名',
  endpoint_port INT UNSIGNED NOT NULL COMMENT '客户端连接 QUIC 的 UDP 端口',
  alpn VARCHAR(64) NOT NULL DEFAULT 'gaccel/1' COMMENT 'QUIC ALPN',
  admin_host VARCHAR(255) NOT NULL DEFAULT '127.0.0.1' COMMENT '节点 admin API 地址，给控制面板探测用',
  admin_port INT UNSIGNED NOT NULL DEFAULT 5557 COMMENT '节点 admin API 端口',
  ssh_host VARCHAR(255) NOT NULL DEFAULT '' COMMENT '控制面板一键部署/更新 SSH 地址',
  ssh_port INT UNSIGNED NOT NULL DEFAULT 22 COMMENT 'SSH 端口',
  ssh_user VARCHAR(64) NOT NULL DEFAULT 'root' COMMENT 'SSH 用户',
  hmac_secret VARCHAR(128) NOT NULL COMMENT '节点 token 签名密钥，业务后台明文保存，不能下发客户端',
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否允许 TCP flow',
  allow_udp TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否允许 UDP flow',
  status ENUM('new','deploying','online','offline','error','disabled') NOT NULL DEFAULT 'new',
  tags JSON NULL COMMENT '标签数组，例如 ["steam","quic"]',
  labels JSON NULL COMMENT '扩展字段对象，例如 {"line":"premium"}',
  desired_version VARCHAR(64) NOT NULL DEFAULT '' COMMENT '期望节点版本，可为空',
  desired_policy_revision VARCHAR(64) NOT NULL DEFAULT '' COMMENT '期望策略版本，可为空',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_node_id (node_id),
  KEY idx_region (region),
  KEY idx_status (status),
  KEY idx_desired_policy (desired_policy_revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='游戏加速节点';
```

## 三、添加节点 SQL 模板

执行前只改变量区即可。`hmac_secret` 建议每个节点独立随机生成，不要所有节点共用。

```sql
-- 变量区：按实际节点修改
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

-- 每个节点生成一次，生成后业务后台要保存。
-- 不要把这个值给客户端，不要写到日志里。
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

-- 首次新增时复制出来存档。已有节点执行 ON DUPLICATE 时不要覆盖旧 hmac_secret。
SELECT @node_id AS node_id, @hmac_secret AS generated_hmac_secret;
```

已有节点只改 IP、端口、线路时，不要重新生成 `hmac_secret`，否则旧客户端 token 会全部失效，节点也需要重写配置。

## 四、同步到控制面板

推荐业务后台用控制面板后端接口同步节点，不推荐直接写控制面板的 `panel_nodes.hmac_secret_encrypted`。

原因：`panel_nodes.hmac_secret_encrypted` 是控制面板用 `security.master_key` 加密后的值，格式类似：

```text
v1:<nonce_base64url>:<ciphertext_base64url>
```

把明文直接写进去会导致一键部署时报：

```text
decrypt node hmac_secret: unsupported encrypted secret format
```

### 同步接口

```http
POST /api/backend/nodes
Authorization: Bearer <backend_api_key>
Content-Type: application/json
```

新增或覆盖同步示例：

```bash
curl -sS -X POST 'http://103.201.131.99:8091/api/backend/nodes' \
  -H 'Authorization: Bearer <backend_api_key>' \
  -H 'Content-Type: application/json' \
  -d '{
    "node_id": "hk-01",
    "name": "香港 01",
    "region": "hk",
    "country": "HK",
    "provider": "aliyun",
    "line_type": "premium",
    "endpoint_host": "47.83.160.126",
    "endpoint_port": 6666,
    "alpn": "gaccel/1",
    "admin_host": "47.83.160.126",
    "admin_port": 5557,
    "ssh_host": "47.83.160.126",
    "ssh_port": 22,
    "ssh_user": "root",
    "allow_tcp": true,
    "allow_udp": true,
    "hmac_secret": "<业务后台保存的节点 hmac_secret>",
    "tags": ["quic"],
    "labels": {"line": "premium"},
    "status": "new",
    "desired_version": "",
    "desired_policy_revision": ""
  }'
```

更新节点时：

```http
PUT /api/backend/nodes/{node_id}
```

注意：

- 不传 `hmac_secret`：控制面板保留旧密钥。
- 传新 `hmac_secret`：控制面板更新加密副本，节点也要重新部署或重写配置。
- `backend_api_key` 是控制面板 `panel.yaml` 里的接口密钥，不是面板登录 JWT。

## 五、如果临时必须直接写控制面板库

只能写节点元数据，不要写 `hmac_secret_encrypted` 明文。写完后仍然要通过 `/api/backend/nodes` 同步一次 `hmac_secret`，或者在面板里走一键部署时输入密钥。

```sql
INSERT INTO panel_nodes (
  node_id, name, region, country, provider, line_type,
  endpoint_host, endpoint_port, alpn,
  admin_host, admin_port,
  ssh_host, ssh_port, ssh_user,
  allow_tcp, allow_udp,
  tags, labels,
  status, desired_version, desired_policy_revision
) VALUES (
  'hk-01', '香港 01', 'hk', 'HK', 'aliyun', 'premium',
  '47.83.160.126', 6666, 'gaccel/1',
  '47.83.160.126', 5557,
  '47.83.160.126', 22, 'root',
  1, 1,
  JSON_ARRAY('quic'), JSON_OBJECT('line', 'premium'),
  'new', '', ''
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
  tags = VALUES(tags),
  labels = VALUES(labels),
  status = VALUES(status),
  desired_version = VALUES(desired_version),
  desired_policy_revision = VALUES(desired_policy_revision),
  updated_at = CURRENT_TIMESTAMP;
```

验证控制面板是否已有加密密钥：

```sql
SELECT
  node_id,
  name,
  endpoint_host,
  endpoint_port,
  IF(hmac_secret_encrypted IS NULL OR hmac_secret_encrypted = '', 0, 1) AS hmac_secret_configured,
  hmac_secret_source,
  hmac_secret_updated_at
FROM panel_nodes
WHERE node_id = 'hk-01';
```

## 六、业务后台签发客户端 token

业务后台给客户端签发 token 时，必须使用对应节点的 `hmac_secret` 明文做 HS256 签名。

JWT claims 建议：

```json
{
  "sub": "user-10001",
  "user_id": "user-10001",
  "device_id": "win-device-01",
  "exp": 1782115200,
  "nbf": 1782111600,
  "iat": 1782111600,
  "max_connections": 64,
  "rate_limit_mbps": 100,
  "allow_tcp": true,
  "allow_udp": true,
  "game_ids": ["steam"],
  "policy_ids": ["steam-web-v1"],
  "config_revision": "20260624.1"
}
```

客户端拿到 token 后只发给节点 `AUTH`，不会也不能解密或验签。

## 七、常见问题

### 1. 业务后台可以直接把 hmac_secret 写到 panel_nodes 吗？

不能写明文到 `hmac_secret_encrypted`。控制面板部署节点时会解密这个字段，明文会报格式不支持。

### 2. 节点到底按什么识别？

按 `node_id`。IP 只是连接入口，可能更换。

### 3. admin_host 是客户端接口吗？

不是。`admin_host/admin_port` 是控制面板诊断节点用的管理口。客户端只用 `endpoint_host/endpoint_port/alpn`。

### 4. SSH 密码保存在哪里？

SSH 凭据属于控制面板运维能力，保存在 `panel_node_credentials`，同样是加密字段。业务后台不要用 SQL 写明文密码，建议由控制面板页面维护。

### 5. 控制面板显示 decrypt secret failed 怎么处理？

这表示控制面板库里的 `panel_nodes.hmac_secret_encrypted` 不是当前 Go 后端能正常解密的密文。常见原因：

- 曾经把明文 `hmac_secret` 直接写进了 `hmac_secret_encrypted`。
- 更换过 `panel.yaml` 的 `security.master_key`，旧密文无法用新 master key 解密。
- 导入/迁移数据库时截断或污染了密文字段。

推荐处理方式：

1. 业务后台确认自己保存的节点明文 `hmac_secret` 还在。
2. 控制面板管理员进入节点列表或节点详情，点击“节点密钥”。
3. 如果状态是 `decrypt_failed` 或 `unsupported_format`，在弹窗里重新同步业务后台保存的明文 `hmac_secret`。
4. 同步成功后重新部署节点，或者确保节点配置里的 `auth.hmac_secret` 与业务后台保存的同一份密钥一致。

不要再用 SQL 直接写 `panel_nodes.hmac_secret_encrypted` 明文。确实需要清空损坏副本时，可以先在面板“节点密钥”弹窗执行清空，再由业务后台接口重新同步。
