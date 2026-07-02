# 业务后台对接控制面板 API

适用版本：`v0.6.10+`

这份文档给业务后台开发使用。控制面板负责保存节点、策略版本、节点目标策略和节点状态；业务后台负责用户套餐、游戏配置、客户端 token 签发。

## 1. 接入边界

### 业务后台要做的事

- 保存自己的用户、套餐、订单、游戏配置。
- 给每个节点生成并保存 `hmac_secret`。
- 调用控制面板 `/api/backend/*` 同步节点和策略。
- 给客户端签发短期 JWT token。

### 控制面板要做的事

- 保存节点资料。
- 保存节点 `hmac_secret` 的加密副本。
- 一键部署节点时，把同一份 `hmac_secret` 写入节点配置。
- 保存 route policy，并把目标策略下发给节点。
- 接收节点上报，提供同步状态查询。

### 不要混用的密钥

| 名称 | 放在哪里 | 用途 |
| --- | --- | --- |
| `backend_api_key` | 控制面板 `panel.yaml` | 业务后台调用 `/api/backend/*` |
| `hmac_secret` | 业务后台保存一份；控制面板保存加密副本；节点配置保存明文 | 业务后台签发客户端 JWT，节点验证 JWT |
| `node_command.secret` | 控制面板和节点配置 | 节点拉取面板命令时验签 |
| 面板登录 JWT | 浏览器登录控制面板后获得 | 只给控制面板页面用，不给业务后台用 |

## 2. 请求约定

示例 API 地址：

```text
http://103.201.131.99:8091
```

业务后台请求头统一这样写：

```http
Authorization: Bearer <backend_api_key>
Content-Type: application/json
Accept: application/json
```

错误返回统一格式：

```json
{
  "error": {
    "code": "store_error",
    "message": "保存节点失败：数据库缺少字段 hmac_secret_encrypted，请执行对应迁移 SQL"
  }
}
```

常见错误：

| code | 说明 |
| --- | --- |
| `unauthorized` | `backend_api_key` 没传或不正确 |
| `invalid_json` | 请求体不是合法 JSON |
| `invalid_node` | 节点字段不合法 |
| `invalid_hmac_secret` | 节点 HMAC Secret 不合法 |
| `node_not_found` | 节点不存在 |
| `store_error` | 数据库写入或读取失败 |

## 3. 推荐接入顺序

1. `GET /api/backend/system/check` 检查控制面板是否可用。
2. `POST /api/backend/nodes` 或 `PUT /api/backend/nodes/{node_id}` 同步节点。
3. `GET /api/backend/token-defaults` 读取 token 默认档位。
4. 业务后台按用户套餐签发客户端 JWT。
5. 有游戏策略时，先调用 `POST /api/backend/policy-revisions/validate` 校验。
6. 校验通过后调用 `POST /api/backend/policy-revisions` 保存策略。
7. 调用 `POST /api/backend/nodes/{node_id}/desired-policy` 设置节点目标策略。
8. 调用 `GET /api/backend/nodes/{node_id}/sync-status` 看节点是否已同步。

## 4. 系统自检

```http
GET /api/backend/system/check
```

用途：业务后台启动时或运维巡检时调用。看控制面板数据库、密钥、CORS、目录和基础表结构是否正常。

响应示例：

```jsonc
{
  "status": "warning", // ok / warning / error
  "version": "0.6.10",
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

处理建议：

- `status=ok`：可以继续同步节点和策略。
- `status=warning`：可以运行，但要看 `checks[].message`。
- `status=error`：不要继续调用写入接口，先修控制面板。

## 5. 读取 token 默认档位

```http
GET /api/backend/token-defaults
```

用途：业务后台签发客户端 token 前，读取控制面板维护的默认连接数、限速和 TCP/UDP 开关。

响应示例：

```jsonc
{
  "token_defaults": {
    "node_hard_limit": 512, // 控制面板允许的单 token 最大连接数上限
    "plans": [
      {
        "plan_id": "standard", // 业务后台可以映射到自己的套餐
        "name": "普通",
        "max_connections": 64, // 写入 JWT claims.max_connections
        "rate_limit_mbps": 100, // 写入 JWT claims.rate_limit_mbps
        "allow_tcp": true, // 写入 JWT claims.allow_tcp
        "allow_udp": true, // 写入 JWT claims.allow_udp
        "description": "默认游戏加速档位",
        "sort_order": 20
      }
    ]
  }
}
```

注意：

- 这个接口只返回默认值，不签发 token。
- 业务后台可以按套餐覆盖这些值，但不要超过 `node_hard_limit`。
- 已经签发出去的 token 不会因为这里修改而改变，客户端需要重新获取 token。

## 6. 新增节点

```http
POST /api/backend/nodes
```

用途：业务后台新增节点后，同步给控制面板。

请求示例。下面是 `jsonc`，只是为了写注释；实际请求要去掉 `//` 注释。

```jsonc
{
  "node_id": "hk-01", // 节点业务 ID。不要用数据库自增 ID；后续都靠它关联
  "name": "香港 01", // 控制面板展示名
  "region": "hk", // 区域代码，业务后台自己定
  "country": "HK", // 国家或地区，可用于展示
  "provider": "aliyun", // 服务商，可为空
  "line_type": "premium", // 线路类型，可为空
  "endpoint_host": "47.83.160.126", // 客户端连接节点的公网 IP 或域名
  "endpoint_port": 6666, // 客户端连接节点的 QUIC UDP 端口
  "alpn": "gaccel/1", // 默认 gaccel/1
  "admin_host": "47.83.160.126", // 控制面板访问节点 admin 的地址
  "admin_port": 5557, // 节点 admin 端口
  "ssh_host": "47.83.160.126", // 控制面板一键部署/更新使用
  "ssh_port": 22,
  "ssh_user": "root",
  "allow_tcp": true,
  "allow_udp": true,
  "hmac_secret": "业务后台为该节点生成的随机密钥", // 首次同步建议必传
  "tags": ["steam", "quic"], // 可为空数组
  "labels": {
    "line": "premium"
  },
  "status": "new",
  "desired_version": "",
  "desired_policy_revision": ""
}
```

响应示例：

```jsonc
{
  "node": {
    "node_id": "hk-01",
    "name": "香港 01",
    "endpoint_host": "47.83.160.126",
    "endpoint_port": 6666,
    "hmac_secret_configured": true, // true 表示控制面板已经保存加密副本
    "hmac_secret_source": "backend",
    "allow_tcp": true,
    "allow_udp": true,
    "status": "new"
  }
}
```

字段规则：

| 字段 | 是否必填 | 说明 |
| --- | --- | --- |
| `node_id` | 是 | 稳定业务 ID。控制面板、业务后台、节点配置都要一致 |
| `name` | 是 | 展示名称 |
| `endpoint_host` | 是 | 客户端连接节点用 |
| `endpoint_port` | 是 | QUIC UDP 端口，范围 `1-65535` |
| `hmac_secret` | 首次建议必填 | 业务后台生成并保存；控制面板只保存加密副本 |
| `admin_host/admin_port` | 建议填 | 面板诊断节点 admin 用。填公网地址时要确认防火墙允许 |
| `ssh_host/ssh_port/ssh_user` | 建议填 | 面板一键部署和更新用 |
| `tags` | 否 | 字符串数组 |
| `labels` | 否 | JSON 对象，值会转成字符串 |

## 7. 更新节点

```http
PUT /api/backend/nodes/{node_id}
```

用途：业务后台修改节点 IP、端口、线路、展示名、目标版本或目标策略时调用。

请求体和新增节点一样。`node_id` 可以放在路径里，body 里可以不传；如果 body 里传了，必须和路径一致。

密钥处理：

- 不传 `hmac_secret`：控制面板保留旧密钥。
- 传新的 `hmac_secret`：控制面板更新加密副本。节点需要重新部署或更新配置后，新 token 才能验证通过。

最小更新示例：

```jsonc
{
  "name": "香港 01",
  "region": "hk",
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
  "tags": [],
  "labels": {},
  "status": "online"
}
```

## 8. 删除节点

```http
DELETE /api/backend/nodes/{node_id}
```

用途：业务后台删除节点或下架节点时调用。

注意：

- 这里只删除控制面板数据库里的节点记录。
- 不会登录服务器卸载节点服务。
- 已经签发出去的客户端 token 不会自动失效，业务后台需要自己处理用户侧 token 生命周期。

响应：

```json
{
  "status": "ok"
}
```

## 9. 校验策略

```http
POST /api/backend/policy-revisions/validate
```

用途：业务后台保存策略前先校验 YAML。这个接口不会写数据库。

请求示例：

```jsonc
{
  "revision": "20260622.1", // 策略版本，建议和客户端配置版本保持一致
  "sha256": "", // 可不传；传了就必须和 route_policies_yaml 内容匹配
  "base_revision": "20260621.1", // 可选，用于返回差异
  "route_policies_yaml": "route_policies:\n  revision: \"20260622.1\"\n  mode: \"client_decision\"\n  policies: []\n"
}
```

响应示例：

```jsonc
{
  "valid": true,
  "sha256": "7f83b1657ff1fc53b92dc18148a1d65dfa135...",
  "errors": [],
  "warnings": [],
  "summary": {
    "revision": "20260622.1",
    "mode": "client_decision",
    "policy_count": 0,
    "rule_count": 0,
    "relay_rule_count": 0,
    "games": [],
    "policies": [],
    "networks": [],
    "target_types": []
  }
}
```

处理规则：

- `valid=false`：不要保存，也不要下发。
- `warnings` 不为空：可以保存，但建议后台展示给管理员看。
- `sha256` 用来做版本内容对账。
- `summary.mode` 表示节点应用该策略后采用的有效模式；当前默认推荐 `client_decision`。

## 10. 保存策略版本

```http
POST /api/backend/policy-revisions
```

用途：把通过校验的 route policy 保存到控制面板。

请求示例：

```jsonc
{
  "revision": "20260622.1",
  "sha256": "", // 可不填，控制面板会自己计算
  "route_policies_yaml": "route_policies:\n  revision: \"20260622.1\"\n  mode: \"client_decision\"\n  policies: []\n"
}
```

响应示例：

```jsonc
{
  "policy_revision": {
    "revision": "20260622.1",
    "sha256": "7f83b1657ff1fc53b92dc18148a1d65dfa135...",
    "source": "backend",
    "created_at": "2026-06-22T14:30:00+08:00"
  }
}
```

## 11. 设置节点目标策略

```http
POST /api/backend/nodes/{node_id}/desired-policy
```

用途：告诉控制面板“这个节点应该使用哪个策略版本”。

请求示例：

```jsonc
{
  "revision": "20260622.1", // 必须是已保存的策略版本
  "create_task": true, // true 时创建 apply_policy 任务，节点下次拉取命令后应用
  "priority": 100 // 任务优先级，数值越大越靠前
}
```

响应示例：

```jsonc
{
  "node_policy_revision": {
    "node_id": "hk-01",
    "revision": "20260622.1",
    "desired": true,
    "applied": false
  },
  "task": {
    "task_id": "apply-policy-20260622T143000-abcdef",
    "node_id": "hk-01",
    "type": "apply_policy",
    "status": "pending"
  }
}
```

注意：

- 只写 `desired_policy_revision` 不代表节点已经应用。
- 真正确认应用成功，要看下一节的 `policy_state`。

## 12. 查询节点同步状态

```http
GET /api/backend/nodes/{node_id}/sync-status
```

用途：业务后台确认节点版本、策略、密钥和任务状态。

响应示例：

```jsonc
{
  "sync_status": {
    "node_id": "hk-01",
    "version_state": "synced", // 版本同步状态
    "policy_state": "pending", // 策略同步状态
    "current_version": "0.3.3",
    "desired_version": "",
    "current_policy_revision": "20260621.1",
    "desired_policy_revision": "20260622.1",
    "hmac_secret_configured": true,
    "deploy_ready": true,
    "pending_tasks": 1,
    "running_tasks": 0,
    "failed_tasks": 0,
    "recommendations": [
      "策略未同步到目标版本，可查看 apply_policy 任务日志或等待节点下次拉取命令"
    ]
  }
}
```

状态含义：

| 值 | 说明 |
| --- | --- |
| `synced` | 当前值已经等于目标值 |
| `pending` | 已设置目标值，但节点上报还是旧值 |
| `waiting_report` | 有目标值，但节点还没有上报当前值 |
| `not_set` | 没设置目标值 |
| `unknown` | 当前值和目标值都为空 |

业务后台建议：

- `hmac_secret_configured=false`：不要给这个节点签发客户端 token。
- `deploy_ready=false`：不要创建部署任务，先补齐节点密钥或 SSH 凭据。
- `policy_state=synced`：才表示节点已应用目标策略。

## 13. 业务后台给客户端签发 token

控制面板目前不负责给客户端签发 token。业务后台需要自己提供客户端接口，例如：

```http
POST /api/client/accelerate/token
```

请求示例：

```jsonc
{
  "user_id": "user-10001",
  "device_id": "win-device-01",
  "node_id": "hk-01",
  "game_id": "steam",
  "plan_id": "standard"
}
```

业务后台处理流程：

1. 查用户是否有套餐和可用时长。
2. 查节点是否可用，并拿到该节点的 `hmac_secret` 明文。
3. 调用或缓存 `GET /api/backend/token-defaults`，按 `plan_id` 取默认值。
4. 结合游戏授权，生成 JWT claims。
5. 使用节点 `hmac_secret` 进行 HS256 签名。
6. 返回 token 和节点连接信息给客户端。

返回给客户端的建议格式：

```jsonc
{
  "node": {
    "node_id": "hk-01",
    "host": "47.83.160.126",
    "port": 6666,
    "alpn": "gaccel/1"
  },
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_at": "2026-06-22T16:00:00+08:00",
  "config_revision": "20260622.1",
  "game_ids": ["steam"],
  "policy_ids": ["steam-web-v1"]
}
```

JWT claims 建议：

```jsonc
{
  "sub": "user-10001",
  "user_id": "user-10001",
  "device_id": "win-device-01",
  "exp": 1782115200, // 必填，过期时间，Unix 秒
  "nbf": 1782111600, // 可选，不早于该时间生效
  "iat": 1782111600, // 建议填写，签发时间
  "max_connections": 64, // 从 token-defaults 档位读取
  "rate_limit_mbps": 100, // 从 token-defaults 档位读取
  "allow_tcp": true,
  "allow_udp": true,
  "game_ids": ["steam"], // 用户被授权的游戏
  "policy_ids": ["steam-web-v1"], // 用户被授权的策略
  "config_revision": "20260622.1" // 客户端 flow metadata 要保持一致
}
```

注意：

- `exp` 必须有。节点会拒绝没有过期时间的 token。
- token 使用节点自己的 `hmac_secret` 签名，不是 `backend_api_key`。
- `game_ids`、`policy_ids`、`config_revision` 会参与节点策略校验。
- 客户端打开 TCP/UDP flow 时，metadata 里的 `game_id`、`policy_id`、`client_config_revision` 要和 token 对得上。

## 14. 最小联调 curl

### 检查面板

```bash
curl -sS http://103.201.131.99:8091/api/backend/system/check \
  -H "Authorization: Bearer <backend_api_key>"
```

### 同步节点

```bash
curl -sS -X POST http://103.201.131.99:8091/api/backend/nodes \
  -H "Authorization: Bearer <backend_api_key>" \
  -H "Content-Type: application/json" \
  -d '{
    "node_id": "hk-01",
    "name": "香港 01",
    "region": "hk",
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
    "hmac_secret": "<per-node-random-secret>",
    "tags": [],
    "labels": {},
    "status": "new"
  }'
```

### 读取 token 档位

```bash
curl -sS http://103.201.131.99:8091/api/backend/token-defaults \
  -H "Authorization: Bearer <backend_api_key>"
```

### 查询同步状态

```bash
curl -sS http://103.201.131.99:8091/api/backend/nodes/hk-01/sync-status \
  -H "Authorization: Bearer <backend_api_key>"
```
