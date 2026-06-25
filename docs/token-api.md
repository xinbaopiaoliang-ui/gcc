# Token 获取接口

`gaccel-token-api` 是一个最小 token 签发服务，给面板或后端使用。它只负责把服务端保存的 `hmac_secret` 签成短期 token；客户端拿到 token 后仍然直接连接节点的 QUIC 端口。
生产环境建议由业务后台为每个节点生成并保存 `hmac_secret`，同时同步一份加密副本到控制面板用于部署。客户端永远只接收短期 JWT token。

不要把 `hmac_secret` 放进客户端。生产环境也不要把 `gaccel-token-api` 直接暴露给未登录用户。

## 配置

示例文件：`token-api.example.yaml`

```yaml
listen: "127.0.0.1:8088"
hmac_secret: "replace-with-the-same-secret-as-node"
api_keys:
  - "replace-with-a-random-api-key"

token:
  default_ttl: "15m"
  max_ttl: "1h"
  default_max_connections: 2
  max_connections_limit: 8
  default_rate_limit_mbps: 50
  rate_limit_mbps_limit: 200
  allow_tcp: true
  allow_udp: true
```

`hmac_secret` 必须和节点 `/etc/gaccel-node/config.yaml` 里的 `auth.hmac_secret` 一致。
示例里的 `change-me-api-key` 和 `replace-with-*` 占位值会被拒绝，必须换成随机值。

也可以用环境变量传入敏感值：

```bash
export GACCEL_HMAC_SECRET="node-hmac-secret"
export GACCEL_TOKEN_API_KEYS="api-key-1,api-key-2"
```

环境变量会覆盖配置文件里的 `hmac_secret` 和 `api_keys`。

## 启动

源码运行：

```bash
go run ./cmd/gaccel-token-api -config token-api.example.yaml
```

Release 安装后：

```bash
sudo nano /etc/gaccel-node/token-api.yaml
sudo systemctl start gaccel-token-api
sudo systemctl status gaccel-token-api --no-pager
```

建议保持默认监听 `127.0.0.1:8088`，由面板后端在内网调用。

## 健康检查

```bash
curl http://127.0.0.1:8088/health
```

返回：

```json
{"status":"ok"}
```

## 签发 Token

```bash
curl -sS http://127.0.0.1:8088/token \
  -H "Authorization: Bearer your-token-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "dev",
    "device_id": "win-test",
    "ttl_seconds": 3600,
    "max_connections": 2,
    "rate_limit_mbps": 50,
    "allow_tcp": true,
    "allow_udp": true,
    "game_ids": ["steam", "example_game"],
    "policy_ids": ["steam-web-v1", "example-game-realtime-v1"],
    "config_revision": "20260616.1"
  }'
```

返回：

```json
{
  "token": "eyJ...",
  "token_type": "Bearer",
  "user_id": "dev",
  "device_id": "win-test",
  "expires_at": "2026-06-16T11:00:00Z",
  "expires_in_seconds": 3600
}
```

客户端拿到 `token` 后直接连接节点：

```powershell
go run ./cmd/gaccel-probe -addr 195.245.242.9:5555 -token "eyJ..." -mode keepalive -count 4 -interval 15s -timeout 70s -client-id win-test -client-version 0.3.1 -client-platform windows/amd64 -insecure=true
```

## 字段说明

- `user_id`：必填，写入 token 的用户 ID。
- `device_id`：可选，写入 token 的设备 ID。
- `ttl_seconds`：可选，不填使用 `token.default_ttl`，不能超过 `token.max_ttl`。
- `max_connections`：可选，不填使用默认值，不能超过 `token.max_connections_limit`。
- `rate_limit_mbps`：可选，不填使用默认值，不能超过 `token.rate_limit_mbps_limit`。
- `allow_tcp` / `allow_udp`：可选，不能突破服务端策略。例如配置里 `allow_tcp: false` 时，请求不能改成 true。
- `game_ids`：可选，允许该 token 打开的游戏 ID 列表；节点启用 `route_policies` 时会校验。
- `policy_ids`：可选，允许该 token 使用的策略 ID 列表；节点启用 `route_policies` 时会校验。
- `config_revision`：可选，客户端配置版本；如果写入 token，flow metadata 的 `client_config_revision` 必须一致。

## 安全边界

- `gaccel-token-api` 是后端/面板接口，不是公开客户端接口。
- 公网部署时必须放在 HTTPS 和登录鉴权之后。
- `api_keys` 只是最小保护，后续正式面板应改为登录态、权限和审计日志。
- token 应保持短有效期，客户端重连时重新获取。
