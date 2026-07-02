# 控制面板与业务后台接口文档

适用版本：v0.6.8

本文给业务后台使用。控制面板负责节点资产、策略版本、节点 desired policy、部署/更新任务和节点上报状态；业务后台不直接连节点，也不保存节点 SSH 明文。

## 主键和连接方式

业务后台和控制面板联系节点时，统一使用 `node_id` 作为业务主键。

- `node_id`：节点唯一 ID。业务后台、控制面板、节点配置里的 `node.id` 必须一致。
- `endpoint_host` / `endpoint_port`：客户端连接节点 QUIC 的公网入口。它们可以变化，不作为主键。
- `admin_host` / `admin_port`：控制面板探测节点 admin 口使用。若填 `127.0.0.1`，只有面板和节点在同一台服务器或已做反代时才能访问。
- `ssh_host` / `ssh_port` / `ssh_user`：控制面板一键部署、更新使用。业务后台不需要也不应该保存 SSH 明文。

因此业务后台添加或更新节点时，请先保证 `node_id` 稳定，再填写 IP、端口、线路、标签等展示和连接字段。

## 鉴权

业务后台接口统一使用：

```http
Authorization: Bearer <backend_api_key>
Content-Type: application/json
```

`backend_api_key` 来自控制面板后端 `panel.yaml` 的 `security.backend_api_keys`，不要使用节点 `auth.hmac_secret`，也不要使用 `node_command.secret`。

## 系统自检

```http
GET /api/backend/system/check
```

用途：业务后台上线前或定时巡检时，确认控制面板的数据库、CORS、密钥配置、账号、节点表结构和部署目录是否正常。

响应不会回显任何 secret 明文，只返回数量、是否配置、状态和建议。

响应示例：

```json
{
  "status": "warning",
  "version": "0.6.0",
  "config": {
    "listen": "127.0.0.1:18091",
    "public_base_url": "http://103.201.131.99:8091",
    "database_driver": "mysql",
    "backend_api_key_count": 1,
    "session_ttl_seconds": 43200,
    "cors_allowed_origins": ["http://103.201.131.99:9788"]
  },
  "summary": {
    "ok": 11,
    "warning": 1,
    "error": 0,
    "total": 12
  },
  "checks": [
    {
      "key": "database.schema",
      "label": "数据库表结构",
      "status": "ok",
      "message": "必要数据库表已就绪"
    }
  ]
}
```

`status` 含义：

| 值 | 说明 |
| --- | --- |
| `ok` | 可以继续联调。 |
| `warning` | 可运行，但存在配置缺口，例如未配置公网入口或暂未同步节点。 |
| `error` | 必须先修复，例如数据库不可用、缺表、无管理员账号或关键密钥未配置。 |

## 节点写入

### 新增或覆盖节点

```http
POST /api/backend/nodes
PUT /api/backend/nodes/{node_id}
```

核心字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `node_id` | 是 | 节点唯一 ID，业务后台、控制面板、节点配置要一致。 |
| `name` | 是 | 展示名称。 |
| `endpoint_host` | 是 | 客户端连接的节点公网 IP 或域名。 |
| `endpoint_port` | 是 | QUIC 入口端口，例如 `5555`。 |
| `alpn` | 否 | 默认 `gaccel/1`。 |
| `admin_host` / `admin_port` | 否 | 节点本机管理接口地址，默认 `127.0.0.1:5557`。 |
| `ssh_host` / `ssh_port` / `ssh_user` | 否 | 面板部署/更新使用，默认复用入口 Host、端口 `22`、用户 `root`。 |
| `allow_tcp` / `allow_udp` | 否 | 节点能力开关，默认均开启。 |
| `hmac_secret` | 首次建议必填 | 业务后台为该节点生成并保存的 token 签名密钥。控制面板只保存加密副本，并在一键部署时写入节点 `auth.hmac_secret`。 |
| `desired_version` | 否 | 目标节点版本。 |
| `desired_policy_revision` | 否 | 目标策略版本。 |

业务后台推荐只写业务确定的字段。SSH 凭据、部署任务和人工排障字段留给控制面板管理员维护。
`hmac_secret` 由业务后台签发和保存；后续 `PUT /api/backend/nodes/{node_id}` 如果不传 `hmac_secret`，控制面板会保留旧的加密副本。需要轮换密钥时，再传新的 `hmac_secret` 并重新部署节点。

示例：

```json
{
  "node_id": "node-hk-01",
  "name": "Hong Kong 01",
  "region": "hk",
  "country": "HK",
  "provider": "provider",
  "line_type": "premium",
  "endpoint_host": "195.245.242.9",
  "endpoint_port": 5555,
  "alpn": "gaccel/1",
  "allow_tcp": true,
  "allow_udp": true,
  "hmac_secret": "backend-generated-random-secret-per-node",
  "tags": ["steam", "quic"],
  "labels": {
    "line": "premium"
  }
}
```

## 策略版本

### 校验策略

```http
POST /api/backend/policy-revisions/validate
```

用于保存前校验 `route_policies` 是否会被节点接受。接口级错误才返回 4xx；策略语义错误返回 200，并在 `valid=false`、`errors[]` 中说明。

请求：

```json
{
  "revision": "20260618.1",
  "sha256": "",
  "base_revision": "20260617.1",
  "route_policies_yaml": "route_policies:\n  revision: \"20260618.1\"\n  mode: \"client_decision\"\n  policies: []\n"
}
```

响应重点字段：

| 字段 | 说明 |
| --- | --- |
| `valid` | 是否通过节点同语义校验。 |
| `sha256` | 控制面板按 YAML 原文计算的 SHA256。 |
| `errors[]` | 必须修复的问题。 |
| `warnings[]` | 可保存但建议关注的问题。 |
| `summary` | 策略模式、策略数、规则数、游戏 ID、策略 ID、协议、目标类型统计。 |
| `diff` | 当传入 `base_revision` 或 `base_route_policies_yaml` 时返回差异摘要。 |

### 保存策略

```http
POST /api/backend/policy-revisions
```

请求：

```json
{
  "revision": "20260618.1",
  "route_policies_yaml": "route_policies:\n  revision: \"20260618.1\"\n  mode: \"client_decision\"\n  policies: []\n"
}
```

`sha256` 可留空，控制面板会计算并校验。保存前建议先调用 validate 接口。

## 下发节点目标策略

```http
POST /api/backend/nodes/{node_id}/desired-policy
```

请求：

```json
{
  "revision": "20260618.1",
  "create_task": true,
  "priority": 100
}
```

行为：

1. 写入 `panel_nodes.desired_policy_revision`。
2. 写入 `panel_node_policy_revisions`。
3. `create_task=true` 时创建 `apply_policy` 任务，等待节点拉取命令。
4. 节点上报 `route_policies.revision` 匹配后，面板标记 applied。

## 查询节点同步状态

```http
GET /api/backend/nodes/{node_id}/sync-status
```

响应：

```json
{
  "sync_status": {
    "node_id": "node-hk-01",
    "version_state": "synced",
    "policy_state": "pending",
    "current_policy_revision": "20260617.1",
    "desired_policy_revision": "20260618.1",
    "hmac_secret_configured": true,
    "deploy_ready": true,
    "pending_tasks": 1,
    "running_tasks": 0,
    "failed_tasks": 0,
    "recommendations": [
      "策略未同步到目标版本，可查看 apply_policy 任务日志或等待节点下一次拉取命令"
    ]
  }
}
```

状态含义：

| 值 | 说明 |
| --- | --- |
| `synced` | 当前值等于目标值。 |
| `pending` | 已设置目标值，但节点上报仍是旧值。 |
| `waiting_report` | 有目标值，但节点还没有上报当前值。 |
| `not_set` | 未设置目标值。 |
| `unknown` | 当前值和目标值都为空。 |

业务后台推荐轮询或在人工操作后查询该接口。真正判断节点是否已经应用策略，以 `policy_state=synced` 为准；不要只看 `desired_policy_revision` 是否已写入。
`deploy_ready=false` 或 `hmac_secret_configured=false` 表示业务后台还没有把节点 `hmac_secret` 同步给控制面板，此时不要创建部署任务，也不要给客户端签发该节点 token。

## 节点接入诊断

```http
GET /api/panel/nodes/{node_id}/diagnostics
```

该接口属于控制面板管理员接口，使用面板 Bearer JWT，不使用 `backend_api_key`。它会只读探测节点 admin `/health`、`/status`、`/panel/commands`，同时返回最近上报、策略同步、任务队列、SSH 凭据状态和处理建议。

控制面板管理员还可以通过下面的接口创建 QUIC/UDP 内核缓冲区优化任务：

```http
POST /api/panel/nodes/{node_id}/tune-udp-buffer
```

默认写入目标节点 `/etc/sysctl.d/99-gaccel-quic.conf`：

```text
net.core.rmem_max=16777216
net.core.wmem_max=16777216
```

该接口需要节点已保存 SSH 凭据；任务会应用 sysctl、重启 `gaccel-node`、验证本机 `/health` 和 `/status`，并检查最近日志中是否仍有 quic-go UDP buffer 警告。业务后台通常不需要调用该接口，它是运维面板能力。

业务后台通常不需要调用该接口。业务后台只需要：

1. 通过 `POST/PUT /api/backend/nodes` 写入节点。
2. 通过 `POST /api/backend/policy-revisions/validate` 校验策略。
3. 通过 `POST /api/backend/policy-revisions` 保存策略。
4. 通过 `POST /api/backend/nodes/{node_id}/desired-policy` 设为节点目标策略。
5. 通过 `GET /api/backend/nodes/{node_id}/sync-status` 确认是否同步完成。

## 节点上报与命令拉取

节点侧接口继续使用 Backend API Key：

```http
POST /api/nodes/report
GET /api/nodes/commands?node_id=<node_id>
```

节点命令响应会带 `X-Gaccel-Command-Timestamp`、`X-Gaccel-Command-Nonce`、`X-Gaccel-Command-Signature`，节点使用 `node_command.secret` 校验签名。业务后台不要直接参与节点命令签名。

## 安全边界

- 业务后台使用 `backend_api_key` 调控制面板。
- 客户端短期 token 使用业务后台保存的节点 `hmac_secret` 生成，必须与节点 `auth.hmac_secret` 一致；控制面板部署时会把加密保存的同一份密钥写入节点配置。
- 节点命令使用 `node_command.secret` 签名。
- SSH 凭据只保存在控制面板数据库，加密依赖 `security.master_key`。
- 控制面板不会在 API 中回显任何 secret 明文。

## Token 默认档位

适用版本：v0.6.8 起。

业务后台签发客户端短期 JWT 时，应优先读取控制面板保存的默认档位，再按用户套餐、游戏、节点能力进行覆盖。该接口只返回默认授权参数，不返回任何 `hmac_secret` 明文。

```http
GET /api/backend/token-defaults
Authorization: Bearer <backend_api_key>
```

响应示例：

```json
{
  "token_defaults": {
    "node_hard_limit": 512,
    "plans": [
      {
        "plan_id": "trial",
        "name": "免费/测试",
        "max_connections": 32,
        "rate_limit_mbps": 50,
        "allow_tcp": true,
        "allow_udp": true,
        "sort_order": 10
      },
      {
        "plan_id": "standard",
        "name": "普通",
        "max_connections": 64,
        "rate_limit_mbps": 100,
        "allow_tcp": true,
        "allow_udp": true,
        "sort_order": 20
      },
      {
        "plan_id": "advanced",
        "name": "高级",
        "max_connections": 128,
        "rate_limit_mbps": 200,
        "allow_tcp": true,
        "allow_udp": true,
        "sort_order": 30
      },
      {
        "plan_id": "premium",
        "name": "旗舰",
        "max_connections": 256,
        "rate_limit_mbps": 500,
        "allow_tcp": true,
        "allow_udp": true,
        "sort_order": 40
      }
    ]
  }
}
```

字段含义：

| 字段 | 说明 |
| --- | --- |
| `node_hard_limit` | 控制面板校验用的节点硬上限。当前标准为 512，任何档位默认 `max_connections` 都不得超过它。 |
| `plans[].plan_id` | 业务套餐/档位 ID。业务后台可以用它映射用户套餐。 |
| `plans[].max_connections` | 写入 JWT claim `max_connections` 的默认值。 |
| `plans[].rate_limit_mbps` | 写入 JWT claim `rate_limit_mbps` 的默认值。 |
| `plans[].allow_tcp` / `allow_udp` | 写入 JWT claim `allow_tcp`、`allow_udp` 的默认值。 |

控制面板管理员可通过系统页修改这些默认值。业务后台仍然是 token 的签发方：读取默认档位后，使用业务后台保存的节点 `hmac_secret` 签发 JWT，并把 `max_connections`、`rate_limit_mbps`、`allow_tcp`、`allow_udp` 写入 claims。

## 控制面板流量统计

适用版本：v0.6.10 起。

```http
GET /api/panel/traffic/overview?window_hours=24&limit=20
Authorization: Bearer <panel_jwt>
```

说明：

- 该接口是控制面板登录后的 JWT 接口，不使用 `backend_api_key`。
- 数据来自节点上报的 `panel_node_reports.metrics_json`，不需要新增 SQL。
- 主要用于运营和联调排障：查看节点流量、用户流量、flow 事件、游戏/策略事件和策略一致性。
- 业务后台如需读取统计数据，后续应单独开放 `/api/backend/traffic/*` 只读接口，避免把面板账号 JWT 交给业务系统。

响应核心字段：

| 字段 | 说明 |
| --- | --- |
| `traffic.sample_mode` | `window_delta` 表示窗口增量；`latest_cumulative` 表示样本不足，只能展示节点累计值。 |
| `traffic.totals.total_bytes` | 当前窗口汇总 TCP/UDP 上下行字节。 |
| `traffic.totals.active_quic_connections` | 当前活跃客户端 QUIC 连接数。 |
| `traffic.totals.active_tcp_flows` / `active_udp_flows` | 当前活跃 TCP/UDP flow 数。 |
| `traffic.totals.flow_open_errors` | flow 打开失败次数，用于排查策略拒绝、目标拒绝和权限不足。 |
| `traffic.nodes[]` | 节点流量排行和上报年龄。 |
| `traffic.users[]` | `user_id` 维度的流量和活跃连接。 |
| `traffic.flow_events[]` | 按 `network/event/reason/game_id/policy_id` 汇总的事件。 |
| `traffic.policy_consistency[]` | 节点当前策略和目标策略是否一致。 |

## 面板查看 Backend API Key

适用版本：v0.7.6 起。

```http
GET /api/panel/security/backend-api-keys
Authorization: Bearer <panel_jwt>
```

说明：

- 该接口只给控制面板管理员使用，不使用 `backend_api_key` 自身鉴权。
- 返回的是 Go 后端 `panel.yaml` 里 `security.backend_api_keys` 的当前值。
- 页面默认只显示数量，管理员点击“查看”后才请求该接口。
- 每次查看都会写入审计日志，action 为 `panel.security.backend_api_keys.view`。
- 审计日志只记录查看动作和数量，不记录密钥明文。

响应示例：

```json
{
  "count": 1,
  "keys": [
    {
      "index": 1,
      "key": "backend-api-key-plain-text",
      "masked": "back********text",
      "length": 26
    }
  ]
}
```

使用边界：

- `key` 只交给业务后台保存，用于调用 `/api/backend/*`。
- 不要把 `key` 下发给客户端。
- 不要把 `key` 写入节点配置；节点使用的是自己的 `auth.hmac_secret` 和 `panel.api_key`。

## 面板管理员修复节点 HMAC Secret

适用版本：v0.7.7 起。

这些接口是控制面板管理员接口，使用面板登录后的 Bearer JWT，不使用 `backend_api_key`。它们用于处理节点出现 `decrypt secret failed`、`unsupported encrypted secret format`、业务后台曾把明文误写到 `hmac_secret_encrypted`、或控制面板 `security.master_key` 更换后旧密文无法解密的场景。

### 查看节点密钥状态

```http
GET /api/panel/nodes/{node_id}/hmac-secret
Authorization: Bearer <panel_jwt>
```

响应示例：

```json
{
  "hmac_secret": {
    "node_id": "hk-01",
    "configured": true,
    "status": "decrypt_failed",
    "message": "节点 HMAC Secret 加密副本无法解密，请重新同步或清空后由业务后台同步",
    "source": "backend",
    "can_clear": true,
    "can_sync": true
  }
}
```

`status` 含义：

| status | 含义 |
| --- | --- |
| `missing` | 控制面板没有保存该节点密钥。 |
| `ok` | 已保存并可解密，响应会返回短 `secret_fingerprint`，不会返回明文。 |
| `decrypt_failed` | 已保存但无法用当前 `security.master_key` 解密。 |
| `unsupported_format` | 字段内容不是控制面板支持的加密格式，常见原因是误写入明文。 |
| `invalid` | 解密后密钥为空或长度不足。 |
| `secret_box_unavailable` | 控制面板未正确配置 `security.master_key`。 |

### 重新同步节点密钥

```http
PUT /api/panel/nodes/{node_id}/hmac-secret
Authorization: Bearer <panel_jwt>
Content-Type: application/json

{
  "hmac_secret": "业务后台保存的节点明文 hmac_secret"
}
```

说明：

- `hmac_secret` 至少 16 个字符，推荐每个节点独立随机 32 字节以上。
- 控制面板只保存加密副本，响应和审计日志都不会返回明文。
- 同步成功后需要重新部署节点或重新写入节点配置，保证节点 `/etc/gaccel-node/config.yaml` 里的 `auth.hmac_secret` 与业务后台签 token 使用的密钥一致。

### 清空损坏密钥副本

```http
DELETE /api/panel/nodes/{node_id}/hmac-secret
Authorization: Bearer <panel_jwt>
```

清空后该节点会变成“未配置密钥”状态。后续应由业务后台重新调用 `/api/backend/nodes` 同步 `hmac_secret`，或者管理员在面板“节点密钥”弹窗里重新同步。

使用边界：

- 客户端永远不访问这些接口，也不能拿到 `hmac_secret`。
- 业务后台仍然是客户端 token 的签发方，必须保存每个节点的明文 `hmac_secret`。
- 面板这个入口是运维修复入口，不替代业务后台的节点主数据管理。
