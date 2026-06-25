# gaccel QUIC 协议草案

## 连接

- 传输层：QUIC over UDP。
- 默认 ALPN：`gaccel/1`。
- 控制消息：JSON Lines，每条 JSON 以换行结束。
- UDP 转发：QUIC DATAGRAM。
- TCP 转发：每个 TCP flow 使用一条独立 QUIC stream。
- 当前协议版本：`1`。

## 客户端联调流程

客户端第一轮联调建议按这个顺序接：

```text
1. Dial QUIC，启用 DATAGRAM，ALPN 使用 gaccel/1。
2. Open control stream。
3. 发送 HELLO，带协议版本和客户端元信息。
4. 接收 HELLO，读取服务端能力和保活建议。
5. 发送 AUTH，带 token 和客户端元信息。
6. 接收 AUTH_OK。
7. 每 15-30 秒发送 PING，服务端返回 PONG。
8. 需要 UDP 时走 OPEN_UDP + QUIC DATAGRAM。
9. 需要 TCP 时新开 QUIC stream，第一条消息发送 OPEN_TCP，成功后切换为原始 TCP 字节流。
10. 断线或 PING 超时后客户端自行重连，重连时必须重新 AUTH。
```

token 策略：当前版本只在 `AUTH` 时校验 token。已建立连接不会因为 token 在连接期间过期而立刻断开；客户端重连时必须使用仍然有效的新 token。

## HELLO

客户端发送：

```json
{
  "type": "HELLO",
  "version": 1,
  "client_id": "client-instance-1",
  "client_version": "0.3.1",
  "client_platform": "windows/amd64"
}
```

`client_id`、`client_version`、`client_platform` 可选，但客户端联调建议发送，方便 `/sessions` 排查。

服务端返回：

```json
{
  "type": "HELLO",
  "version": 1,
  "server": {
    "alpn": "gaccel/1",
    "protocol_version": 1,
    "capabilities": ["auth_hmac", "udp_datagram", "tcp_stream", "ping", "flow_close_notify"],
    "keepalive_interval_seconds": 15,
    "datagram_header_bytes": 11,
    "recommended_datagram_bytes": 1200,
    "recommended_datagram_payload_bytes": 1189,
    "token_policy": "validated_on_auth"
  }
}
```

## AUTH

客户端发送：

```json
{
  "type": "AUTH",
  "version": 1,
  "token": "dev-token-or-hmac-token",
  "client_id": "client-instance-1",
  "client_version": "0.3.1",
  "client_platform": "windows/amd64"
}
```

服务端返回：

```json
{
  "type": "AUTH_OK",
  "version": 1,
  "user_id": "user-1",
  "device_id": "device-1",
  "server": {
    "alpn": "gaccel/1",
    "protocol_version": 1,
    "capabilities": ["auth_hmac", "udp_datagram", "tcp_stream", "ping", "flow_close_notify"],
    "keepalive_interval_seconds": 15,
    "datagram_header_bytes": 11,
    "recommended_datagram_bytes": 1200,
    "recommended_datagram_payload_bytes": 1189,
    "token_policy": "validated_on_auth"
  }
}
```

失败时：

```json
{"type":"ERROR","error_code":"token_expired","error":"token expired"}
```

## HMAC Token

生产测试可以使用 HMAC/JWT 风格 token。服务端配置：

```yaml
auth:
  mode: "hmac"
  hmac_secret: "replace-with-a-long-random-secret"
  token_leeway: "30s"
```

token 使用 HS256，常用 claims：

```json
{
  "sub": "user-1",
  "user_id": "user-1",
  "device_id": "device-1",
  "iat": 1781510400,
  "nbf": 1781510395,
  "exp": 1781511300,
  "max_connections": 2,
  "rate_limit_mbps": 50,
  "allow_tcp": true,
  "allow_udp": true
}
```

`exp` 必须存在；`nbf` 和 `iat` 会按 `auth.token_leeway` 做时钟偏差容忍。`max_connections`、`rate_limit_mbps`、`allow_tcp`、`allow_udp` 会覆盖服务端默认限制。

生成示例：

```bash
gaccel-token -secret "replace-with-a-long-random-secret" -user user-1 -device device-1 -ttl 15m -game-ids steam -policy-ids steam-web-v1 -config-revision 20260616.1
```

## PING

客户端建议每 15-30 秒发送一次：

```json
{"type":"PING"}
```

服务端返回：

```json
{"type":"PONG"}
```

服务端会记录最近一次 PING 时间到 `/sessions.last_ping_at`。客户端连续多次 PING 超时后应主动关闭连接并重连。

## OPEN_UDP

客户端通过控制 stream 请求创建 UDP flow：

```json
{"type":"OPEN_UDP","target_host":"8.8.8.8","target_port":53}
```

服务端返回：

```json
{"type":"OPEN_UDP","flow_id":1}
```

后续该 UDP flow 的数据通过 QUIC DATAGRAM 传输。

## QUIC DATAGRAM 格式

```text
version    1 byte
type       1 byte
flow_id    4 bytes, big endian
seq        4 bytes, big endian
flags      1 byte
payload    n bytes
```

当前 `type`：

```text
1 = UDP payload
```

建议单个 QUIC DATAGRAM 总长度控制在 `1200` 字节以内，payload 建议不超过 `1189` 字节。

## OPEN_TCP

客户端打开一条新的 QUIC stream，并把第一条消息作为 TCP 打开请求：

```json
{"type":"OPEN_TCP","target_host":"example.com","target_port":443}
```

服务端返回：

```json
{"type":"OPEN_TCP","flow_id":2}
```

收到成功响应后，这条 QUIC stream 切换为原始 TCP 字节流。客户端必须等待成功响应后再发送 TCP payload，避免 JSON decoder 预读后续原始字节。

服务端在 TCP flow 关闭后会尝试打开一条新的控制 stream，并发送关闭通知：

```json
{"type":"CLOSE_FLOW","flow_id":2,"error_code":"eof"}
```

常见关闭原因：

```text
eof
closed
error
session_closed
```

## CLOSE_FLOW

当前版本主要用于显式关闭 UDP flow：

```json
{"type":"CLOSE_FLOW","flow_id":1}
```

TCP flow 会随对应 QUIC stream 关闭而释放。

## 错误码

鉴权相关：

```text
auth_failed
token_expired
token_not_active
token_missing_exp
token_invalid
unauthorized
max_connections_exceeded
```

转发相关：

```text
permission_denied
target_denied
rate_limited
max_flows_exceeded
open_udp_failed
open_tcp_failed
unknown_message
```

客户端应按 `error_code` 分支处理，不要依赖 `error` 文本。

## 服务端安全策略

服务端会拒绝：

- 未鉴权连接。
- 未授权端口。
- 私网、本机、链路本地、多播地址。
- 云元数据地址，例如 `169.254.169.254`。
- 超过节点最大连接数、单用户最大连接数或单连接最大 flow 数的请求。

## 管理接口

默认监听 `127.0.0.1:9090`。

```text
GET /health
GET /status
GET /sessions
GET /panel/commands
GET /metrics
POST /config/reload
GET /debug/pprof/
```

`/status` 会输出节点状态、监听地址、节点元数据、指标快照和最近面板命令执行结果。节点元数据来自配置文件的 `node` 段：

```json
{
  "node": {
    "id": "node-hk-01",
    "region": "hk",
    "tags": ["steam", "quic"],
    "labels": {
      "provider": "example",
      "line": "premium"
    }
  }
}
```

`/sessions` 会输出在线连接、flow、user_id、device_id、client_id、client_version、client_platform、protocol_version、last_ping_at、connected_duration_seconds、有效连接上限、有效限速和 TCP/UDP 权限。

`/panel/commands` 会输出最近的面板命令执行结果，便于确认 `config_reload`、`apply_config`、`apply_policy` 和 `stage_upgrade` 是否成功：

```json
{
  "commands": [
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

`/config/reload` 会重新读取启动时的配置文件路径。新鉴权、新连接、新 flow 会读取最新配置；监听地址、TLS 证书和 QUIC listener 级参数需要重启进程才能完全生效。

## 面板上报 Payload

完整面板对接说明见：[面板对接协议](./panel.md)。

可选配置 `panel.report_url` 后，节点会定时 POST 状态到面板。该接口是节点主动上报，不做客户端订阅下发。

请求头：

```http
Authorization: Bearer <panel.api_key>
Content-Type: application/json
User-Agent: gaccel-node/<version>
```

请求体示例：

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
  "metrics": {
    "active_quic_connections": 0,
    "active_udp_flows": 0,
    "active_tcp_flows": 0
  }
}
```

面板返回任意 `2xx` 即认为成功。非 `2xx` 或请求失败会记录 warning 日志，下个周期继续上报。

## 测试工具

`gaccel-probe` 仅用于协议验证，不是正式客户端。

Rust 客户端开发可参考：[Rust 客户端联调指南](./rust-client.md)。

PING：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode ping -client-id dev-client -client-version 0.3.1 -client-platform windows/amd64
```

保活：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode keepalive -count 4 -interval 15s -timeout 70s -client-id dev-client
```

UDP：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode udp -target-host 8.8.8.8 -target-port 53 -payload test -count 10
```

TCP：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode tcp -target-host example.com -target-port 80 -payload "GET / HTTP/1.0\r\n\r\n"
```
