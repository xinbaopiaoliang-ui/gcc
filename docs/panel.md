# 面板对接协议

本文档描述节点和面板之间的最小对接方式。

当前设计原则：

- 节点主动访问面板，管理 API 不暴露公网。
- heartbeat/report 只上报状态，不做下发。
- 运维命令由节点主动拉取，面板响应必须带 HMAC 签名。
- 当前命令支持 `noop`、`config_reload`、`apply_config`、`apply_policy` 和 `stage_upgrade`。

## 浏览器面板鉴权

v0.5.9 起，前端静态站和 Go 后端可以分开部署。浏览器面板人工操作使用账号登录后返回的短期 Bearer JWT，不再依赖跨站 Cookie。

登录：

```http
POST /api/panel/login
Content-Type: application/json
```

```json
{
  "username": "admin",
  "password": "your-panel-password"
}
```

响应：

```json
{
  "token_type": "Bearer",
  "access_token": "eyJ...",
  "token": "eyJ...",
  "expires_at": "2026-06-17T20:00:00+08:00",
  "expires_in_seconds": 43200,
  "user": {
    "username": "admin",
    "role": "admin",
    "status": "active"
  }
}
```

后续 `/api/panel/*` 请求必须携带：

```http
Authorization: Bearer <panel_access_token>
```

当前分离部署建议：

- 前端 PHP 静态站：`http://103.201.131.99:9788`
- Go 后端 API：`http://103.201.131.99:8091`
- 前端通过 `panel-config.js` 设置 `apiBaseURL`。
- Go 后端通过 `cors.allowed_origins` 或 `GACCEL_PANEL_CORS_ALLOWED_ORIGINS` 放行前端 Origin。

注意：业务后台同步接口 `/api/backend/*`、节点上报 `/api/nodes/report` 和节点命令拉取 `/api/nodes/commands` 仍使用独立 `backend_api_key`，不要使用面板登录 JWT。

## 配置

```yaml
node:
  id: "node-hk-01"
  region: "hk"
  tags:
    - "steam"
    - "quic"
  labels:
    provider: "example"
    line: "premium"

panel:
  report_url: "https://panel.example.com/api/nodes/report"
  command_url: "https://panel.example.com/api/nodes/commands"
  api_key: "replace-with-panel-api-key"
  command_secret: "replace-with-random-command-secret"
  interval: "30s"
  timeout: "10s"
  command_interval: "30s"
  command_timeout: "10s"
  command_max_clock_skew: "2m"

upgrade:
  stage_dir: "/var/lib/gaccel-node/upgrades"
  max_package_bytes: 209715200
  timeout: "2m"
  allow_http: false
```

`report_url` 和 `command_url` 都是可选项。为空时对应能力不会启动。

## 节点状态上报

节点定时向 `panel.report_url` 发送 `POST`。

请求头：

```http
Authorization: Bearer <panel.api_key>
Content-Type: application/json
User-Agent: gaccel-node/<version>
```

请求体：

```json
{
  "status": "ok",
  "version": "0.3.2",
  "timestamp": "2026-06-16T12:00:00Z",
  "node": {
    "id": "node-hk-01",
    "region": "hk",
    "tags": ["steam", "quic"],
    "labels": {
      "provider": "example",
      "line": "premium"
    }
  },
  "server": {
    "listen": ":5555",
    "alpn": "gaccel/1"
  },
  "route_policies": {
    "revision": "20260616.1",
    "policy_count": 2
  },
  "metrics": {
    "active_quic_connections": 0,
    "active_udp_flows": 0,
    "active_tcp_flows": 0,
    "udp_client_to_target_bytes": 0,
    "udp_target_to_client_bytes": 0,
    "tcp_client_to_target_bytes": 0,
    "tcp_target_to_client_bytes": 0,
    "users": [],
    "flow_events": []
  },
  "panel_commands": [
    {
      "id": "cmd-stage-upgrade-1",
      "type": "stage_upgrade",
      "ok": true,
      "details": {
        "version": "0.3.3",
        "sha256": "7f0d...64hex",
        "size_bytes": 12345678,
        "file_path": "/var/lib/gaccel-node/upgrades/0.3.3/gaccel-node_0.3.3_linux-amd64.tar.gz",
        "manifest_path": "/var/lib/gaccel-node/upgrades/0.3.3/manifest.json",
        "staged_at": "2026-06-16T12:00:00Z"
      },
      "executed_at": "2026-06-16T12:00:00Z"
    }
  ]
}
```

面板返回任意 `2xx` 即认为成功。非 `2xx` 或请求失败会记录 warning 日志，下个周期继续上报。
`panel_commands` 为空时会省略；有命令执行后会携带最近的执行结果，面板可据此确认运维命令成功或失败。

`route_policies.revision` 和 `route_policies.policy_count` 用于面板确认节点当前加载的策略版本，尤其是 `apply_policy` 下发后做对账。

### gaccel-panel 已实现的上报处理

控制面板 `gaccel-panel` 当前实现了：

```http
POST /api/nodes/report
```

处理规则：

- 必须带 `Authorization: Bearer <backend_api_key>`。
- `node.id` 必须已经存在于 `panel_nodes.node_id`。
- 每次上报写入 `panel_node_reports`。
- 同步更新节点最新状态、版本、策略版本、最后上报时间和最后错误。
- 如果上报包含 `panel_commands`，会按命令 `id` 回写 `panel_node_tasks` 的执行结果。

## 运维命令拉取

节点定时向 `panel.command_url` 发送 `GET`。

节点会自动追加 `node_id` query 参数：

```text
GET /api/nodes/commands?node_id=node-hk-01
```

请求头：

```http
Authorization: Bearer <panel.api_key>
Accept: application/json
User-Agent: gaccel-node/<version>
```

无命令时，面板可以返回：

```http
204 No Content
```

有命令时，面板返回 `200 OK` 和 JSON body：

```json
{
  "commands": [
    {
      "id": "cmd-20260616-0001",
      "type": "config_reload",
      "issued_at": "2026-06-16T12:00:00Z",
      "expires_at": "2026-06-16T12:02:00Z",
      "payload": {}
    }
  ]
}
```

## 命令响应签名

面板返回命令时必须带签名头：

```http
X-Gaccel-Timestamp: 2026-06-16T12:00:00Z
X-Gaccel-Nonce: random-nonce-128bit
X-Gaccel-Signature: v1=<hex-hmac-sha256>
```

签名输入：

```text
timestamp + "\n" + nonce + "\n" + raw_body
```

签名算法：

```text
HMAC-SHA256(key = panel.command_secret)
```

输出格式：

```text
v1=<lowercase hex digest>
```

面板伪代码：

```text
body = JSON.stringify(command_envelope)
message = timestamp + "\n" + nonce + "\n" + body
signature = "v1=" + hex(hmac_sha256(command_secret, message))
```

节点校验：

- `X-Gaccel-Timestamp` 必须是 RFC3339 时间。
- 时间戳必须在 `panel.command_max_clock_skew` 内，默认 `2m`。
- `X-Gaccel-Nonce` 在时间窗内不能重复。
- `X-Gaccel-Signature` 必须和本地计算值一致。
- 命令 `expires_at` 过期后不会执行。

### gaccel-panel 已实现的命令队列

控制面板 `gaccel-panel` 当前实现了：

```http
GET /api/nodes/commands?node_id=<node_id>
GET /api/panel/nodes/{node_id}/reports?limit=20
GET /api/panel/nodes/{node_id}/tasks?limit=20
POST /api/panel/nodes/{node_id}/commands/apply_policy
```

`GET /api/nodes/commands` 会领取 `pending` 任务并置为 `running`。无任务返回 `204 No Content`，有任务返回带 HMAC 签名的命令 envelope。

`POST /api/panel/nodes/{node_id}/commands/apply_policy` 请求体：

```json
{
  "revision": "20260617.1",
  "sha256": "",
  "route_policies_yaml": "route_policies:\n  revision: \"20260617.1\"\n  policies: []\n"
}
```

`sha256` 可留空；为空时控制面板会按 `route_policies_yaml` 原始 UTF-8 内容自动计算。

### gaccel-panel 已实现的策略版本闭环

v0.5.5 起，业务后台推荐走策略版本闭环，而不是直接给节点塞一段临时 YAML：

```http
GET /api/backend/policy-revisions?limit=50
POST /api/backend/policy-revisions
POST /api/backend/nodes/{node_id}/desired-policy
GET /api/panel/policy-revisions?limit=50
POST /api/panel/nodes/{node_id}/desired-policy
```

`POST /api/backend/policy-revisions` 请求体：

```json
{
  "revision": "20260617.1",
  "sha256": "",
  "route_policies_yaml": "route_policies:\n  revision: \"20260617.1\"\n  policies: []\n"
}
```

规则：

- `revision` 是业务后台生成的策略版本，控制面板用它做唯一键。
- `sha256` 可留空；如果填写，必须等于 `route_policies_yaml` 原始 UTF-8 字节的 SHA256。
- 业务后台接口写入的 `source` 固定为 `backend`，面板手动接口写入的 `source` 固定为 `manual`。

`POST /api/backend/nodes/{node_id}/desired-policy` 请求体：

```json
{
  "revision": "20260617.1",
  "create_task": true,
  "priority": 100
}
```

处理规则：

- 控制面板先检查 `node_id` 和 `revision` 是否存在。
- 成功后更新 `panel_nodes.desired_policy_revision`。
- 同步写入 `panel_node_policy_revisions`，把该节点其他 desired 策略置为非目标。
- `create_task=true` 时自动创建 `apply_policy` 任务，节点下一次 `GET /api/nodes/commands` 会拉取。
- 节点后续上报相同 `route_policies.revision` 时，控制面板自动把该节点策略标记为 `applied=true` 并写入 `applied_at`。

### gaccel-panel 已实现的 SSH 运维任务

以下接口由控制面板后台通过 SSH 主动连接节点服务器执行，和节点主动拉取的 `/api/nodes/commands` 是两条不同链路：

```http
GET /api/panel/nodes/{node_id}/credential
PUT /api/panel/nodes/{node_id}/credential
DELETE /api/panel/nodes/{node_id}/credential
POST /api/panel/nodes/{node_id}/credential/test
POST /api/panel/nodes/{node_id}/deploy
POST /api/panel/nodes/{node_id}/update
GET /api/panel/tasks/{task_id}/logs?limit=300
```

`POST /api/panel/nodes/{node_id}/update` 请求体：

```json
{
  "version": "v0.4.6"
}
```

更新任务会先备份 `/usr/local/bin/gaccel-*`、`/etc/systemd/system/gaccel-node.service` 和 `/etc/gaccel-node/config.yaml` 到 `/var/lib/gaccel-node/backups/<task_id>`，再由目标节点服务器拉取 GitHub release install script 安装指定版本。重启后必须通过节点本机 `health`、`status` 和版本检查；失败时会尝试恢复备份并重启。

## 支持命令

### noop

用于联调签名和通道连通性。

```json
{
  "id": "cmd-noop-1",
  "type": "noop",
  "issued_at": "2026-06-16T12:00:00Z",
  "expires_at": "2026-06-16T12:02:00Z",
  "payload": {}
}
```

### config_reload

触发节点重新读取启动时的配置文件路径，等价于本机管理接口 `POST /config/reload`。

```json
{
  "id": "cmd-reload-1",
  "type": "config_reload",
  "issued_at": "2026-06-16T12:00:00Z",
  "expires_at": "2026-06-16T12:02:00Z",
  "payload": {}
}
```

注意：

- 新鉴权、新连接、新 flow 会读取最新配置。
- 监听地址、TLS 证书和 QUIC listener 级参数仍需要重启节点进程才能完全生效。
- 如果配置校验失败，命令不会生效，节点会保留旧配置并记录 warning。

### apply_config

下发完整配置包，节点会做 SHA256 校验、YAML 配置校验、原子写入、热重载和失败回滚。

```json
{
  "id": "cmd-apply-config-1",
  "type": "apply_config",
  "issued_at": "2026-06-16T12:00:00Z",
  "expires_at": "2026-06-16T12:02:00Z",
  "payload": {
    "sha256": "7f0d...64hex",
    "config_yaml": "server:\n  listen: \":5555\"\n  alpn: \"gaccel/1\"\n  cert_file: \"/etc/gaccel-node/cert.pem\"\n  key_file: \"/etc/gaccel-node/key.pem\"\n..."
  }
}
```

`sha256` 必须按 `config_yaml` 的原始 UTF-8 字节计算。注意不要按 JSON 转义后的字符串计算。

节点处理顺序：

```text
1. 校验面板命令响应 HMAC 签名。
2. 校验命令未过期。
3. 校验 payload.sha256 == sha256(payload.config_yaml)。
4. 使用节点本地配置解析器预加载 config_yaml。
5. 配置合法时，写入 <config>.rollback 备份旧配置。
6. 原子替换当前 config.yaml。
7. 调用热重载。
8. 如果热重载失败，恢复旧 config.yaml，并恢复旧内存配置。
```

成功后，节点日志会记录 `panel command executed`。失败时，节点会记录 warning，旧配置继续生效。

限制：

- `apply_config` 只能更新新鉴权、新连接、新 flow 使用的运行时配置。
- QUIC 监听地址、TLS 证书路径和 listener 级参数仍需要重启节点进程才能完全生效。
- 配置包不应包含面板命令密钥的明文回传日志。
- 面板侧需要保存每次配置包的 SHA256、操作者、发布时间和目标节点。

### apply_policy

只下发节点 `route_policies` 策略块，适合游戏策略频繁变更时使用。节点会做 SHA256 校验、YAML 策略校验、原子写入、热重载和失败回滚。

```json
{
  "id": "cmd-apply-policy-1",
  "type": "apply_policy",
  "issued_at": "2026-06-16T12:00:00Z",
  "expires_at": "2026-06-16T12:02:00Z",
  "payload": {
    "sha256": "7f0d...64hex",
    "route_policies_yaml": "route_policies:\n  revision: \"20260616.1\"\n  policies:\n    - policy_id: \"steam-web-v1\"\n      game_id: \"steam\"\n      allow_tcp: true\n      allow_udp: false\n      rules: []\n"
  }
}
```

`route_policies_yaml` 可以是带顶层 `route_policies:` 的 YAML，也可以直接是：

```yaml
revision: "20260616.1"
policies: []
```

`sha256` 必须按 `route_policies_yaml` 的原始 UTF-8 字节计算。成功结果会包含 `sha256`、`backup_path` 和 `revision`。失败时旧策略继续生效。

### stage_upgrade

下发节点二进制升级包的暂存任务。节点会下载升级包、校验 SHA256、写入 `upgrade.stage_dir/<version>/`，并生成 `manifest.json`。

该命令不会直接替换 `/usr/local/bin/gaccel-node`，也不会自动重启服务。前期流程建议由面板先完成暂存，再由人工或后续受控安装器执行切换版本，避免远程命令直接打断正在运行的节点。

```json
{
  "id": "cmd-stage-upgrade-1",
  "type": "stage_upgrade",
  "issued_at": "2026-06-16T12:00:00Z",
  "expires_at": "2026-06-16T12:02:00Z",
  "payload": {
    "version": "0.3.3",
    "url": "https://github.com/xinbaopiaoliang-ui/gcc/releases/download/v0.3.3/gaccel-node_0.3.3_linux-amd64.tar.gz",
    "sha256": "7f0d...64hex",
    "file": "gaccel-node_0.3.3_linux-amd64.tar.gz"
  }
}
```

字段说明：

- `version` 是暂存目录名，只允许字母、数字、点、下划线、短横线和加号。
- `url` 默认必须是 HTTPS；只有显式配置 `upgrade.allow_http: true` 时才允许 HTTP。
- `sha256` 必须按升级包原始字节计算，支持可选的 `sha256:` 前缀。
- `file` 可选；为空时节点会使用 URL path 的 basename。文件名只允许字母、数字、点、下划线和短横线。

成功结果会包含 `version`、`sha256`、`size_bytes`、`file_path`、`manifest_path` 和 `staged_at`。面板可以根据节点上报的 `version` 判断是否需要下发 `stage_upgrade`。

## 安全建议

- `panel.api_key` 和 `panel.command_secret` 必须是不同随机值。
- `command_secret` 不要下发给客户端。
- 面板命令接口必须走 HTTPS。
- 命令必须设置较短 `expires_at`。
- 面板侧需要记录命令 ID、操作者、目标节点、签名时间和执行结果。
- 配置包和升级包都必须带 SHA256；升级包默认只允许 HTTPS 下载。
- `stage_upgrade` 只负责暂存和校验，不负责替换二进制或重启节点。生产环境切换版本前应再次核对 manifest 和发布来源。
