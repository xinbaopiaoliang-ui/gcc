# 临时控制面板开发计划

## 目标

控制面板用于节点运维和业务后台同步，不替代业务后台的用户、套餐、游戏授权系统。

第一版目标是快速形成闭环：

```text
业务后台
  -> 同步节点资料、游戏策略、策略版本
  -> 请求控制面板部署、更新、同步策略

控制面板
  -> 管理节点运维资料
  -> 保存 SSH 凭据
  -> 一键部署节点
  -> 一键更新节点
  -> 接收节点 heartbeat/report
  -> 给节点返回 apply_policy / apply_config / stage_upgrade 等命令

服务器节点
  -> 首次部署由控制面板 SSH 进入服务器完成
  -> 安装后主动上报状态
  -> 主动拉取面板命令
  -> 按 node_id 和控制面板保持同步
```

当前控制面板开发/测试目标服务器：

```text
host: 103.201.131.99
environment: Baota panel + MySQL 8.0.45
backend_path: /www/wwwroot/go/gaccel-panel
backend_listen: 127.0.0.1:18091
backend_public_api: http://103.201.131.99:8091
frontend_site: http://103.201.131.99:9788
process: Baota Go project / gaccelpanel
```

当前 v0.5.9 目标部署模式：

- MySQL 数据库 `gaccel_panel` 已创建，schema 已导入。
- 前端静态文件独立放在宝塔 PHP 项目，访问地址为 `http://103.201.131.99:9788`。
- Go 后端独立放在宝塔 Go 项目，执行文件为 `/www/wwwroot/go/gaccel-panel/gaccel-panel`。
- Go 后端本机监听 `127.0.0.1:18091`，公网 API 入口由宝塔或端口映射提供为 `http://103.201.131.99:8091`。
- 浏览器面板登录后使用短期 Bearer JWT 调用 `/api/panel/*`。
- 业务后台同步接口和节点上报/命令接口继续使用独立 Bearer API Key，不和面板登录 JWT 混用。
- 前端通过 `panel-config.js` 配置 `apiBaseURL`，当前目标值为 `http://103.201.131.99:8091`。

## 技术选型

### 后端

- 语言：Go
- 服务名：`gaccel-panel`
- HTTP 路由：先用 Go 标准库 `net/http`，等接口复杂后再考虑引入轻量 router。
- SSH：`golang.org/x/crypto/ssh`
- 配置：YAML + 环境变量覆盖。
- 任务执行：Go 内置 worker + MySQL 任务表。
- 日志：`log/slog`。

选择原因：

- 当前节点内核已经是 Go，复用工程、构建、部署和并发模型成本最低。
- 一键部署、一键更新、状态上报、命令拉取都属于 I/O 密集任务，Go 的并发模型适合。
- 后续可以把控制面板和节点内核保持同一个 release 工程风格。

### 前端

- React
- TypeScript
- Vite
- Ant Design

选择原因：

- 控制面板是典型后台系统，核心是表格、表单、弹窗、任务日志、状态筛选。
- Ant Design 的表格、表单、布局、反馈组件适合快速做运维后台。
- Vite 适合轻量、快速构建，不需要一开始上 Next.js。

### 数据库

- MySQL 8.0.45

选择原因：

- 业务后台前面已经按 MySQL 表结构设计，控制面板用 MySQL 可以直接复用 `node_id`、`policy_id`、`config_revision` 等模型。
- 第一版任务队列、审计日志、节点状态都可以落 MySQL，不需要额外 Redis。
- 用户当前服务器已经安装 MySQL 8.0.45，第一版只使用 MySQL 8.0 兼容能力：InnoDB、JSON、ENUM、DATETIME、普通索引和唯一索引。

## 边界划分

### 业务后台负责

- 用户账号、套餐、计费。
- 游戏、进程、规则、策略生成。
- 用户授权。
- 调用 token API 签发客户端连接节点的短期 token。
- 把节点资料、策略版本同步给控制面板。

### 控制面板负责

- 节点 CRUD。
- 节点 SSH 凭据管理。
- 节点首次部署。
- 节点更新。
- 节点重启、状态检查。
- 接收节点 heartbeat/report。
- 保存节点最新状态。
- 下发节点命令。
- 调用 `apply_policy` 同步策略。
- 保存任务日志和操作审计。

### 节点负责

- QUIC Relay。
- HMAC token 鉴权。
- `route_policies` 强校验。
- `/status`、`/sessions`、`/metrics`。
- 主动上报控制面板。
- 主动拉取控制面板命令。

## 节点身份模型

节点身份只用 `node_id`，不能用 IP 当主键。

```text
node_id        节点唯一身份
endpoint_host  客户端连接地址，可以是 IP 或域名
endpoint_port  客户端连接 QUIC 端口
admin_host     控制面板内部探测地址，默认不暴露公网
admin_port     节点本地 admin 端口
ssh_host       SSH 部署地址
ssh_port       SSH 端口
```

节点上报时必须带：

```json
{
  "node": {
    "id": "node-test-01",
    "region": "test",
    "tags": ["steam", "quic"]
  },
  "version": "0.4.6-local",
  "route_policies": {
    "revision": "20260616.1",
    "policy_count": 2
  }
}
```

## 安全原则

### SSH 凭据

第一版允许输入账号密码，但必须按以下规则处理：

- 密码不允许明文输出到日志。
- 数据库里保存 `password_encrypted`，不保存明文。
- 加密密钥从环境变量 `GACCEL_PANEL_MASTER_KEY` 读取。
- 支持一次性密码：部署成功后清空密码。
- 支持 SSH key，生产环境优先使用 SSH key。
- 每次使用凭据都写审计日志。

### 控制面板访问

第一版必须至少支持：

- 登录。
- Bearer JWT。
- 管理员账号。
- 操作审计。
- 只监听内网或反向代理后面。

正式生产前必须补：

- HTTPS 强制。
- RBAC。
- 登录失败限制。
- 双人确认高危操作。
- 凭据轮换。

## 数据库表设计

### panel_users

```sql
CREATE TABLE panel_users (
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
```

### panel_nodes

```sql
CREATE TABLE panel_nodes (
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
```

### panel_node_credentials

```sql
CREATE TABLE panel_node_credentials (
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
```

### panel_node_reports

```sql
CREATE TABLE panel_node_reports (
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
```

### panel_node_tasks

```sql
CREATE TABLE panel_node_tasks (
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
```

### panel_node_task_logs

```sql
CREATE TABLE panel_node_task_logs (
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
```

### panel_policy_revisions

```sql
CREATE TABLE panel_policy_revisions (
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
```

### panel_node_policy_revisions

```sql
CREATE TABLE panel_node_policy_revisions (
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
```

### panel_audit_logs

```sql
CREATE TABLE panel_audit_logs (
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
```

## 控制面板 API

### 登录

```http
POST /api/panel/login
POST /api/panel/logout
GET /api/panel/me
```

### 节点 CRUD

```http
GET /api/panel/nodes
POST /api/panel/nodes
GET /api/panel/nodes/{node_id}
PUT /api/panel/nodes/{node_id}
DELETE /api/panel/nodes/{node_id}
```

### 节点凭据

```http
PUT /api/panel/nodes/{node_id}/credential
GET /api/panel/nodes/{node_id}/credential
DELETE /api/panel/nodes/{node_id}/credential
POST /api/panel/nodes/{node_id}/credential/test
```

v0.5.3 已实现凭据保存和连接测试：

- 凭据使用 `security.master_key` 派生 AES-GCM 密钥加密后写入 `panel_node_credentials`。
- 支持 `password` 与 `private_key` 两种认证方式。
- API 响应只返回 `has_password`、`has_private_key`、`last_used_at` 等状态，不返回密码或私钥明文。
- `POST /credential/test` 只建立 SSH 连接并执行轻量探测命令，不安装、不更新、不改服务器文件。

### 节点任务

```http
POST /api/panel/nodes/{node_id}/deploy
POST /api/panel/nodes/{node_id}/update
POST /api/panel/nodes/{node_id}/restart
POST /api/panel/nodes/{node_id}/health-check
POST /api/panel/nodes/{node_id}/apply-policy
GET /api/panel/tasks
GET /api/panel/tasks/{task_id}
GET /api/panel/tasks/{task_id}/logs
```

v0.5.3 已实现 `deploy` 和任务日志：

```http
POST /api/panel/nodes/{node_id}/deploy
Content-Type: application/json

{
  "version": "latest",
  "panel_base_url": "http://103.201.131.99:8091"
}
```

部署任务规则：

- v0.6.4 起，`hmac_secret` 由业务后台生成并保存，通过 `POST/PUT /api/backend/nodes` 同步给控制面板；控制面板仅保存加密副本。
- 一键部署不再要求人工输入 `hmac_secret`，部署时自动读取节点记录里的加密副本并写入目标节点 `/etc/gaccel-node/config.yaml`，任务日志不会输出密钥。
- 如果节点未同步 `hmac_secret`，同步状态会返回 `hmac_secret_configured=false`、`deploy_ready=false`，部署任务会拒绝执行。
- 面板通过已保存的 SSH 凭据连接节点服务器。
- 当前部署只允许写入既定节点路径：`/usr/local/bin/gaccel-*`、`/etc/gaccel-node`、`/var/lib/gaccel-node`、`/etc/systemd/system/gaccel-node.service`。
- 部署完成后会检查目标节点本机 `http://127.0.0.1:<admin_port>/health`、`/status` 和 `/usr/local/bin/gaccel-node -version`。
- 任务日志通过 `GET /api/panel/tasks/{task_id}/logs?limit=300` 查询。

v0.5.4 已实现 `update` 和失败回滚：

```http
POST /api/panel/nodes/{node_id}/update
Content-Type: application/json

{
  "version": "v0.4.6"
}
```

更新任务规则：

- 面板通过已保存的 SSH 凭据连接节点服务器，缺少凭据时返回 `credential_required`。
- 更新前备份 `/usr/local/bin/gaccel-*`、`/etc/systemd/system/gaccel-node.service` 和 `/etc/gaccel-node/config.yaml` 到 `/var/lib/gaccel-node/backups/<task_id>`。
- 当前实现由目标节点服务器拉取 GitHub release install script 安装指定版本，不允许面板上传任意路径文件。
- 更新后会重启 `gaccel-node`，并检查目标节点本机 `http://127.0.0.1:<admin_port>/health`、`/status` 和 `/usr/local/bin/gaccel-node -version`。
- 验证失败时自动恢复备份二进制并重启，任务结果会记录 `rolled_back` 和 `backup_dir`。

### 业务后台同步接口

业务后台使用 API key 调用，不使用面板登录 JWT。

```http
POST /api/backend/nodes
PUT /api/backend/nodes/{node_id}
DELETE /api/backend/nodes/{node_id}
POST /api/backend/policy-revisions
POST /api/backend/nodes/{node_id}/desired-policy
```

v0.5.1 已实现节点同步接口，请求头统一为：

```http
Authorization: Bearer <backend_api_key>
Content-Type: application/json
```

`POST /api/backend/nodes` 和 `PUT /api/backend/nodes/{node_id}` 请求体：

```json
{
  "node_id": "node-hk-01",
  "name": "Hong Kong 01",
  "region": "hk",
  "country": "HK",
  "provider": "test",
  "line_type": "premium",
  "endpoint_host": "195.245.242.9",
  "endpoint_port": 5555,
  "alpn": "gaccel/1",
  "admin_host": "127.0.0.1",
  "admin_port": 5557,
  "ssh_host": "195.245.242.9",
  "ssh_port": 22,
  "ssh_user": "root",
  "allow_tcp": true,
  "allow_udp": true,
  "tags": ["steam", "quic"],
  "labels": {
    "provider": "test",
    "line": "premium"
  },
  "status": "new",
  "desired_version": "v0.4.6",
  "desired_policy_revision": "20260616.1"
}
```

可省略字段默认值：

```text
alpn=gaccel/1
admin_host=127.0.0.1
admin_port=5557
ssh_host=endpoint_host
ssh_port=22
ssh_user=root
allow_tcp=true
allow_udp=true
status=new
```

控制面板侧节点 CRUD 使用面板登录后返回的 Bearer JWT：

```http
Authorization: Bearer <panel_access_token>
```

接口如下：

```http
GET /api/panel/nodes?q=<keyword>&status=<status>&region=<region>&limit=100&offset=0
POST /api/panel/nodes
GET /api/panel/nodes/{node_id}
PUT /api/panel/nodes/{node_id}
DELETE /api/panel/nodes/{node_id}
```

### 节点上报和命令拉取

复用现有节点协议：

```http
POST /api/nodes/report
GET /api/nodes/commands?node_id=<node_id>
```

节点上报和命令拉取使用同一个临时 Bearer API key：

```http
Authorization: Bearer <backend_api_key>
```

`POST /api/nodes/report` 已实现：

- 校验节点必须先存在于 `panel_nodes`。
- 写入 `panel_node_reports` 历史快照。
- 更新 `panel_nodes.status`、`current_version`、`current_policy_revision`、`last_report_at` 和 `last_error`。
- 解析 `panel_commands` 执行结果，回写 `panel_node_tasks.status`、`result_json`、`error_message`、`finished_at`。

控制面板侧查询接口已实现：

```http
GET /api/panel/nodes/{node_id}/reports?limit=20
GET /api/panel/nodes/{node_id}/tasks?limit=20
POST /api/panel/nodes/{node_id}/commands/apply_policy
```

`POST /api/panel/nodes/{node_id}/commands/apply_policy` 请求体：

```json
{
  "revision": "20260617.1",
  "sha256": "",
  "route_policies_yaml": "route_policies:\n  revision: \"20260617.1\"\n  policies: []\n"
}
```

`sha256` 可留空，控制面板会按 `route_policies_yaml` 原始 UTF-8 内容自动计算。任务进入 `pending` 后，节点下一次拉取命令会把任务置为 `running`。

`GET /api/nodes/commands` 返回的数据必须带现有 HMAC 签名头：

```http
X-Gaccel-Timestamp
X-Gaccel-Nonce
X-Gaccel-Signature
```

## 一键部署流程

```text
1. 面板创建 deploy_node 任务。
2. worker 读取节点 SSH 凭据。
3. SSH 连接服务器。
4. 检查系统和架构。
5. 创建 gaccel 用户、目录和权限。
6. 上传或下载 gaccel-node、gaccel-probe、gaccel-token、gaccel-token-api。
7. 写入 /etc/gaccel-node/config.yaml。
8. 写入 TLS cert/key。
9. 写入 systemd service。
10. systemctl daemon-reload。
11. systemctl enable --now gaccel-node。
12. curl 127.0.0.1:<admin_port>/health。
13. curl 127.0.0.1:<admin_port>/status。
14. 成功后更新 panel_nodes.status=online。
15. 如果是一性密码，清空 password_encrypted。
```

部署必须只动以下路径：

```text
/usr/local/bin/gaccel-node
/usr/local/bin/gaccel-probe
/usr/local/bin/gaccel-token
/usr/local/bin/gaccel-token-api
/etc/gaccel-node
/var/lib/gaccel-node
/etc/systemd/system/gaccel-node.service
/etc/systemd/system/gaccel-token-api.service
```

## 一键更新流程

第一版使用 SSH 临时更新：

```text
1. 面板创建 update_node 任务。
2. 面板读取已保存 SSH 凭据并连接目标节点服务器。
3. 备份 /usr/local/bin/gaccel-*、gaccel-node.service 和 config.yaml 到 /var/lib/gaccel-node/backups/<task_id>/。
4. 目标服务器拉取 GitHub release install script 并安装指定版本。
5. systemctl restart gaccel-node。
6. 验证 version、health、status。
7. 失败时自动恢复备份二进制并重启。
```

正式版再切换为节点主动 `stage_upgrade` + 受控切换。

## 策略同步流程

```text
1. 业务后台生成 route_policies_yaml。
2. 业务后台调用 POST /api/backend/policy-revisions。
3. 控制面板保存 revision、sha256、yaml。
4. 业务后台指定 node_id 的 desired_policy_revision。
5. 控制面板创建 apply_policy 任务。
6. 如果节点在线并能拉命令，优先走 GET /api/nodes/commands。
7. 如果节点未接入命令拉取，可以临时通过 SSH 写入策略并重启。
8. 节点上报 route_policies.revision。
9. 控制面板确认 desired == current。
```

## 前端页面

### 登录页

- 用户名。
- 密码。
- 登录失败提示。

### 节点列表

- 搜索：node_id、名称、IP、地区。
- 筛选：状态、地区、策略版本、版本号。
- 列：node_id、endpoint、状态、版本、策略版本、连接数、最后上报时间。
- 操作：详情、部署、更新、策略同步、重启。

### 节点详情

- 基础信息。
- 连接信息。
- SSH 凭据状态。
- 最新 heartbeat/report。
- 当前 metrics。
- sessions 摘要。
- 当前策略版本。
- 最近任务。
- 最近错误。

### 节点表单

- node_id。
- 名称。
- endpoint_host / endpoint_port。
- region / country / provider / line_type。
- QUIC ALPN。
- SSH host / port / user。
- TCP/UDP 开关。
- tags / labels。

### 凭据弹窗

- password / private_key 选择。
- 一次性凭据开关。
- 测试连接按钮。
- 不显示明文回填。

### 任务日志页

- 任务状态。
- 步骤时间线。
- stdout/stderr。
- 错误原因。
- 重试按钮。

## 开发阶段

### v0.5.0 控制面板文档与骨架

- [x] 写入本控制面板开发计划。
- [x] 新增 `cmd/gaccel-panel`。
- [x] 新增 panel 配置加载。
- [x] 新增 MySQL schema 文件。
- [x] 新增基础 HTTP server。
- [x] 提供 `/health`。

### v0.5.1 节点 CRUD 与业务后台同步

- [x] 节点 CRUD API。
- [x] 业务后台 node 同步 API。
- [x] 节点列表和详情页。
- [x] 操作审计。
- [x] 在 `103.201.131.99` 导入 MySQL schema 并部署 `gaccel-panel`。
- [x] 通过服务器本机 `/health` 和 `/api/panel/nodes` 验证服务与数据库。
- [x] 确认公网访问入口：`103.201.131.99:8090` 已放行，健康检查、401 鉴权保护和带 key 节点列表均验证通过。
- [x] 部署 React 控制面板页面：支持节点列表、筛选、创建/编辑、详情抽屉和删除。

### v0.5.2 节点 report 与 command

- [x] 接收节点 heartbeat/report。
- [x] 保存最新节点状态。
- [x] 实现节点命令拉取。
- [x] 支持 `apply_policy` 命令队列。
- [x] 节点详情页展示最近上报和最近任务。
- [x] 节点列表与详情页支持创建 `apply_policy` 任务。

### v0.5.3 SSH 凭据与一键部署

- [x] SSH 凭据加密保存。
- [x] SSH 连接测试。
- [x] 控制面板页面支持保存、删除和测试 SSH 凭据。
- [x] 已部署到 `103.201.131.99`：`gaccel-panel` 版本 `0.5.3-local`，公网 `/health` 返回 ok，授权节点列表 200，凭据接口 200，未授权访问 401。
- [x] 一键部署任务：`POST /api/panel/nodes/{node_id}/deploy` 创建 `deploy_node` 任务，并由面板后台通过 SSH 执行。
- [x] 部署日志：`GET /api/panel/tasks/{task_id}/logs` 查询 `panel_node_task_logs`。
- [x] 部署后 health/status 验证：部署完成后检查节点本机 `/health`、`/status` 和二进制版本，并回写任务结果与节点状态。
- [x] 控制面板页面支持创建部署任务和查看任务日志。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.3-local`，前端页面 200，授权节点列表 200，任务日志接口 200，部署接口参数校验 400，未授权访问 401。

### v0.5.4 一键更新与回滚

- [x] `POST /api/panel/nodes/{node_id}/update` 创建 `update_node` 任务。
- [x] 备份旧二进制、systemd service 和节点配置。
- [x] 目标服务器拉取 release install script 安装指定版本并重启服务。
- [x] health/status/version 验证失败自动回滚并记录任务结果。
- [x] 控制面板页面支持创建更新任务和查看任务日志。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.4-local`，前端页面 200，授权节点列表 200，任务日志接口 200，更新接口参数校验 400，未授权访问 401。

### v0.5.5 策略同步闭环

- [x] 保存策略版本：`GET/POST /api/panel/policy-revisions` 与 `GET/POST /api/backend/policy-revisions`。
- [x] 业务后台提交 desired policy：`POST /api/backend/nodes/{node_id}/desired-policy`。
- [x] 控制面板下发 `apply_policy`：节点详情页可选择已保存策略版本并创建同步任务。
- [x] 节点上报 revision 后自动确认完成：`panel_node_policy_revisions.applied` 和 `applied_at` 自动回写。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.5-local`，公网首页 200，策略版本保存/列表接口已验证，Supervisor 进程 running。

### v0.5.6 面板登录与 Session

- [x] 面板登录接口：`POST /api/panel/login`、`POST /api/panel/logout`、`GET /api/panel/me`。
- [x] 面板人工操作接口使用 `gaccel_panel_session` HttpOnly Cookie，不再在页面填写 Backend API Key。
- [x] 业务后台同步接口和节点上报/命令接口继续使用 Bearer API Key，避免把机器密钥暴露给浏览器页面。
- [x] 新增 `gaccel-panel -create-admin`，可创建或重置 `panel_users` 管理员 bcrypt 密码。
- [x] React 控制面板新增登录页、登录态检查、右上角当前账号和退出登录。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.6-local`，`admin` 登录成功，未登录 `/api/panel/nodes` 返回 401，登录后 `/api/panel/nodes` 返回 200，公网首页 200。

### v0.5.7 面板登录安全加固

- [x] 登录失败限流：按 `IP + username` 统计失败登录，连续失败后返回 `login_rate_limited`。
- [x] 当前账号修改密码：`POST /api/panel/me/password`，需要当前密码，新密码写入 bcrypt hash。
- [x] 改密成功刷新 Session Cookie，并写入 `panel.password.change` 审计。
- [x] React 控制面板右上角新增“改密”入口和修改密码弹窗。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.7-local`，失败登录第 6 次返回 429，改密后旧密码登录 401，恢复后 admin 登录 200。

### v0.5.8 账号管理与角色权限

- [x] 账号管理 API：`GET/POST /api/panel/users`、`PUT /api/panel/users/{id}`、`POST /api/panel/users/{id}/password`。
- [x] 角色边界：`admin` 可执行写操作和高危运维动作；`operator`、`viewer` 只开放节点、上报、任务和日志查看。
- [x] 自保护规则：管理员不能在账号管理里降级/禁用自己，也不能通过账号管理重置自己的密码。
- [x] React 控制面板新增“账号与权限”页面，支持新建账号、编辑角色/状态、重置密码。
- [x] 节点列表和详情页根据角色隐藏编辑、凭据、部署、更新、策略下发和删除入口。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.8-local`；管理员账号管理接口 200；临时 operator 可登录并查看节点列表；operator 访问账号管理和创建节点均返回 403；禁用后登录返回 401。

### v0.5.9 前后端分离与 JWT 面板登录

- [x] 面板人工操作接口改为 `Authorization: Bearer <panel_access_token>`，浏览器不再依赖跨站 Cookie。
- [x] `POST /api/panel/login` 和 `POST /api/panel/me/password` 返回短期 JWT，前端保存后续请求自动携带。
- [x] 后端保留旧 `gaccel_panel_session` Cookie 解析兼容，但新前端不再写入 Cookie。
- [x] 后端新增 `cors.allowed_origins`，允许 `http://103.201.131.99:9788` 直接调用 `http://103.201.131.99:8091`。
- [x] 前端新增 `panel-config.js` 运行时配置，默认 `apiBaseURL` 指向 `http://103.201.131.99:8091`。
- [x] 前端新增节点弹窗重新排版，创建节点时不再把示例默认值直接填进输入框。
- [x] 打包拆分为前端静态包和 Go 后端包，分别适配宝塔 PHP 项目和 Go 项目。
- [x] 本地验证通过：`go test ./internal/panel ./cmd/gaccel-panel` 与 `web/panel npm run build`。

## 可行性判断

### 技术可行性

高。

现有节点已经具备：

- 节点元数据。
- heartbeat/report 客户端。
- command 拉取客户端。
- `apply_config`。
- `apply_policy`。
- `stage_upgrade`。
- `/status` 和 `/sessions`。

控制面板主要是把这些能力串成后台、数据库和任务系统。

### 风险

中高。

主要风险不是技术，而是安全：

- SSH 密码保存。
- root 权限部署。
- 错误任务误操作服务器。
- 面板 API 暴露。
- 高危操作缺少审计。

第一版必须限制使用范围：只给内部联调和受控服务器使用。

### 推荐落地顺序

1. 先做 `gaccel-panel` 后端骨架和数据库。
2. 再做节点 CRUD。
3. 再接节点 report/command。
4. 再做 SSH 部署。
5. 最后做一键更新和批量策略同步。

不要先做复杂 UI。先让 API 和任务系统跑通，再完善页面。
