# 客户端与节点联调协议说明

本文档给客户端开发使用，目标是把客户端和 `gaccel-node` 的联调行为约束清楚。

阅读对象可以是人，也可以是 AI 开发助手。实现时请严格按本文字段、顺序和错误处理执行，不要自行推断隐藏行为。

v0.6.0 联调时，建议同时阅读 `docs/v0.6.0-integration-checklist.md`，用于确认业务后台、控制面板、节点和客户端四方字段是否一致。

## 范围

本文只描述客户端和节点之间的协议与联调要求。

客户端本机如何捕获流量可以使用 WFP、WinDivert、TUN、进程代理、系统代理或其他方案，但这些都属于客户端内部实现。节点只接收客户端已经判定需要加速的 flow。

节点不会识别客户端本机进程。进程识别必须在客户端本机完成。

## 基本原则

- 客户端必须按业务后台下发的游戏配置做分流，不能默认全局转发。
- TCP 和 UDP 都必须支持。
- 每个 TCP flow 使用独立 QUIC bidirectional stream。
- UDP flow 使用控制消息创建，再用 QUIC DATAGRAM 传输 payload。
- 客户端必须先 `HELLO`，再 `AUTH`，鉴权成功后才能打开 TCP/UDP flow。
- 客户端发给节点的 `metadata.process_name` 只用于日志和排查，节点不能只靠进程名放行。
- 节点放行依据是：token 授权 + 节点本地策略 + OPEN 请求里的目标地址和元信息。
- 客户端必须处理服务端 `ERROR.error_code`，不要依赖 `ERROR.error` 文本。

## 传输层

| 项 | 要求 |
| --- | --- |
| 协议 | QUIC over UDP |
| 默认 ALPN | `gaccel/1` |
| 最低 TLS | TLS 1.3 |
| 控制消息编码 | JSON Lines，每条 JSON 后必须追加 `\n` |
| TCP relay | QUIC bidirectional stream |
| UDP relay | QUIC DATAGRAM |
| 当前协议版本 | `1` |

联调环境如果节点使用自签证书，客户端可以临时跳过节点证书校验。生产环境必须使用可信证书或证书固定。

## 客户端启动输入

客户端至少需要从业务后台拿到：

```json
{
  "revision": "20260616.1",
  "device_id": "win-device-1",
  "games": [],
  "nodes": [
    {
      "node_id": "node-test-01",
      "endpoint_host": "195.245.242.9",
      "endpoint_port": 5555,
      "alpn": "gaccel/1",
      "sni": "",
      "insecure": true,
      "token": "short-lived-jwt",
      "policies": ["steam-web-v1"]
    }
  ]
}
```

字段要求：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `revision` | 是 | 客户端配置版本。必须放入 flow metadata。 |
| `device_id` | 是 | 当前设备 ID。应和 token 内 device_id 一致。 |
| `games` | 是 | 游戏、进程、TCP/UDP 路由规则。 |
| `nodes` | 是 | 可用节点列表。 |
| `nodes[].node_id` | 是 | 节点 ID。 |
| `nodes[].endpoint_host` | 是 | 节点地址。 |
| `nodes[].endpoint_port` | 是 | 节点 QUIC 端口。 |
| `nodes[].alpn` | 是 | 通常为 `gaccel/1`。 |
| `nodes[].sni` | 可选 | 节点证书 SNI。 |
| `nodes[].token` | 是 | 短期 JWT token。 |
| `nodes[].policies` | 是 | 该节点允许当前客户端使用的策略 ID。 |

## 连接状态机

客户端必须按这个状态机实现：

```text
Idle
  -> LoadConfig
  -> SelectNode
  -> DialQUIC
  -> OpenControlStream
  -> HELLO
  -> AUTH
  -> Ready
  -> OpenTCP / OpenUDP

Ready 状态：
  - 定时 PING。
  - 按进程和规则打开 flow。
  - 连接断开后进入 Backoff。

Backoff:
  - 刷新 token 或复用仍有效 token。
  - 重新 DialQUIC。
```

重连退避建议：

```text
第 1 次: 300ms
第 2 次: 800ms
第 3 次: 1500ms
之后: 3000-5000ms，并加随机抖动
```

客户端禁止无限快速重连。

## HELLO

客户端连接 QUIC 后，必须打开一条 control stream，发送 `HELLO`：

```json
{
  "type": "HELLO",
  "version": 1,
  "client_id": "win-device-1-session-1",
  "client_version": "0.4.0",
  "client_platform": "windows/amd64"
}
```

字段要求：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | 是 | 固定 `HELLO`。 |
| `version` | 是 | 当前固定 `1`。 |
| `client_id` | 强烈建议 | 客户端实例 ID，用于节点 `/sessions` 排查。 |
| `client_version` | 强烈建议 | 客户端版本。 |
| `client_platform` | 强烈建议 | 例如 `windows/amd64`。 |

节点返回：

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

客户端必须读取并保存：

- `server.protocol_version`
- `server.capabilities`
- `server.keepalive_interval_seconds`
- `server.recommended_datagram_payload_bytes`

如果 `capabilities` 不包含客户端需要的能力，应停止使用该节点。

## AUTH

`HELLO` 成功后，客户端在同一条 control stream 上发送 `AUTH`：

```json
{
  "type": "AUTH",
  "version": 1,
  "token": "short-lived-jwt",
  "client_id": "win-device-1-session-1",
  "client_version": "0.4.0",
  "client_platform": "windows/amd64"
}
```

节点返回：

```json
{
  "type": "AUTH_OK",
  "version": 1,
  "user_id": "user-1",
  "device_id": "win-device-1",
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

客户端必须记录 `user_id` 和 `device_id`，用于和节点 `/sessions` 对账。

## PING

鉴权成功后，客户端必须定时发送：

```json
{"type":"PING"}
```

节点返回：

```json
{"type":"PONG"}
```

建议：

- 默认每 15 秒发送一次，优先使用节点 `HELLO.server.keepalive_interval_seconds`。
- 单次 PING 超过 5 秒无响应视为失败。
- 连续 2-3 次失败后关闭 QUIC 并重连。

## 游戏规则命中

客户端本机捕获到连接后，必须先做规则匹配。

匹配输入：

```text
process_name
network: tcp/udp
target_host 或 target_ip
target_port
```

匹配输出：

```json
{
  "game_id": "steam",
  "policy_id": "steam-web-v1",
  "rule_id": "steam-store-tcp-443",
  "action": "quic_relay"
}
```

如果没有命中 `action=quic_relay` 的规则，客户端必须直连或按业务配置处理，不能默认发送到节点。

## OPEN_TCP

每个 TCP flow 必须打开一条新的 QUIC bidirectional stream。

发送第一条 JSON Line：

```json
{
  "type": "OPEN_TCP",
  "target_host": "store.steampowered.com",
  "target_port": 443,
  "metadata": {
    "game_id": "steam",
    "policy_id": "steam-web-v1",
    "rule_id": "steam-store-tcp-443",
    "network": "tcp",
    "process_name": "steamwebhelper.exe",
    "process_path_hash": "optional-sha256",
    "client_config_revision": "20260616.1",
    "capture_mode": "process",
    "trace_id": "optional-client-generated-id"
  }
}
```

节点成功返回：

```json
{
  "type": "OPEN_TCP",
  "flow_id": 1
}
```

客户端收到成功响应后，这条 QUIC stream 立刻切换为原始 TCP 字节流。

严格要求：

- 发送 `OPEN_TCP` 后必须先读取一条 JSON Line 响应。
- 收到 `OPEN_TCP` 成功前，不能写入 TCP payload。
- 成功后不能再把这条 stream 当 JSON 控制流使用。
- 读取 JSON 响应时不能使用会预读后续原始字节的 decoder。

## OPEN_UDP

UDP flow 在 control stream 上创建。

客户端发送：

```json
{
  "type": "OPEN_UDP",
  "target_host": "203.0.113.10",
  "target_port": 27015,
  "metadata": {
    "game_id": "example_game",
    "policy_id": "example-game-realtime-v1",
    "rule_id": "example-game-udp-27000-27999",
    "network": "udp",
    "process_name": "game.exe",
    "process_path_hash": "optional-sha256",
    "client_config_revision": "20260616.1",
    "capture_mode": "process",
    "trace_id": "optional-client-generated-id"
  }
}
```

节点成功返回：

```json
{
  "type": "OPEN_UDP",
  "flow_id": 2
}
```

后续 UDP payload 通过 QUIC DATAGRAM 发送。

## Metadata 字段

`OPEN_TCP` 和 `OPEN_UDP` 都必须带 `metadata`。字段如下：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `game_id` | 是 | 游戏 ID，例如 `steam`、`pubg`、`lol`。 |
| `policy_id` | 是 | 后台策略 ID。 |
| `rule_id` | 是 | 命中的具体规则 ID。 |
| `network` | 是 | `tcp` 或 `udp`，必须和 OPEN 类型一致。 |
| `process_name` | 建议 | 本机进程名，用于日志和排查。 |
| `process_path_hash` | 可选 | 进程路径 hash。 |
| `client_config_revision` | 是 | 客户端配置版本。 |
| `capture_mode` | 建议 | `process`、`wfp`、`windivert`、`tun` 等。 |
| `trace_id` | 可选 | 客户端生成的链路追踪 ID。 |

节点启用 `route_policies` 后会校验：

```text
metadata.game_id 是否被 token 允许
metadata.policy_id 是否被 token 允许
metadata.rule_id 是否属于节点本地策略
metadata.network 是否和消息类型一致
target_host + target_port 是否命中节点策略 rule
```

## UDP DATAGRAM

DATAGRAM 二进制格式：

```text
version    1 byte, 当前 1
type       1 byte, UDP payload = 1
flow_id    4 bytes, big endian
seq        4 bytes, big endian
flags      1 byte, 当前 0
payload    n bytes
```

客户端发送 UDP payload：

```text
datagram.version = 1
datagram.type = 1
datagram.flow_id = OPEN_UDP 返回的 flow_id
datagram.seq = 递增序号
datagram.flags = 0
datagram.payload = 原始 UDP payload
```

客户端接收 DATAGRAM 时必须：

- 校验长度至少 11 字节。
- 校验 `version == 1`。
- 校验 `type == 1`。
- 按 `flow_id` 分发给对应 UDP flow。
- 丢弃未知 flow 的 datagram。

payload 大小：

- 优先使用 `HELLO.server.recommended_datagram_payload_bytes`。
- 当前推荐 payload 不超过 `1189` 字节。
- 超过大小时客户端应自行分片、降级或丢弃，不要阻塞主循环。

## CLOSE_FLOW

UDP flow 可以显式关闭：

```json
{
  "type": "CLOSE_FLOW",
  "flow_id": 2
}
```

TCP flow 随对应 QUIC stream 关闭释放。

节点可能通过新的控制 stream 通知 flow 关闭：

```json
{
  "type": "CLOSE_FLOW",
  "flow_id": 1,
  "error_code": "eof"
}
```

客户端收到未知 `flow_id` 的关闭通知时应忽略。

## 错误处理

节点错误格式：

```json
{
  "type": "ERROR",
  "error_code": "target_denied",
  "error": "target denied"
}
```

客户端必须按 `error_code` 处理。

| error_code | 客户端动作 |
| --- | --- |
| `token_expired` | 刷新 token，重新连接节点。 |
| `token_not_active` | 等待或刷新 token，重新连接节点。 |
| `token_missing_exp` | 后台签发错误，停止使用该 token。 |
| `token_invalid` | 后台签发错误或密钥不匹配，停止使用该 token。 |
| `auth_failed` | 停止重试，向业务后台上报。 |
| `unauthorized` | 未鉴权就发业务消息，客户端实现错误。 |
| `max_connections_exceeded` | 关闭旧连接或等待后重试。 |
| `permission_denied` | token 不允许该 network。 |
| `target_denied` | 规则或目标不被节点允许。不要自动改成全局转发。 |
| `rate_limited` | 降低发送速率，尤其是 UDP。 |
| `max_flows_exceeded` | 关闭闲置 flow 或新建连接。 |
| `open_tcp_failed` | 单个 TCP 目标打开失败，可以按规则重试。 |
| `open_udp_failed` | 单个 UDP 目标打开失败，可以按规则重试。 |
| `unknown_message` | 客户端协议错误，停止当前连接并上报。 |

## token Claims

正式联调阶段建议 token 包含：

```json
{
  "sub": "user-1",
  "user_id": "user-1",
  "device_id": "win-device-1",
  "exp": 1781593681,
  "nbf": 1781590076,
  "iat": 1781590081,
  "max_connections": 2,
  "rate_limit_mbps": 50,
  "allow_tcp": true,
  "allow_udp": true,
  "game_ids": ["steam", "example_game"],
  "policy_ids": ["steam-web-v1", "example-game-realtime-v1"],
  "config_revision": "20260616.1"
}
```

当前节点版本已经支持：

- `user_id`
- `device_id`
- `exp`
- `nbf`
- `iat`
- `max_connections`
- `rate_limit_mbps`
- `allow_tcp`
- `allow_udp`
- `game_ids`
- `policy_ids`
- `config_revision`

当节点配置了 `route_policies` 时，`game_ids`、`policy_ids` 和 `config_revision` 会参与 `OPEN_TCP` / `OPEN_UDP` 强校验。

## 客户端日志字段

每条连接和 flow 日志至少包含：

```text
timestamp
node_id
node_addr
client_id
client_version
client_platform
user_id
device_id
game_id
policy_id
rule_id
network
flow_id
target_host
target_port
process_name
client_config_revision
trace_id
latency_ms
error_code
```

这些字段必须能和节点 `/sessions`、`/status`、`flow_events` 对齐。

## TCP 联调用例

Steam 商店：

```json
{
  "type": "OPEN_TCP",
  "target_host": "store.steampowered.com",
  "target_port": 443,
  "metadata": {
    "game_id": "steam",
    "policy_id": "steam-web-v1",
    "rule_id": "steam-store-tcp-443",
    "network": "tcp",
    "process_name": "steamwebhelper.exe",
    "client_config_revision": "20260616.1",
    "capture_mode": "process"
  }
}
```

成功标准：

```text
1. OPEN_TCP 返回 flow_id。
2. 客户端原始 TCP/TLS 字节能正常双向转发。
3. Steam 客户端商店页面能加载。
4. 节点 /sessions 能看到 client_id 和 tcp flow。
```

## UDP 联调用例

示例游戏 UDP：

```json
{
  "type": "OPEN_UDP",
  "target_host": "103.201.131.99",
  "target_port": 15555,
  "metadata": {
    "game_id": "example_game",
    "policy_id": "example-game-realtime-v1",
    "rule_id": "example-game-udp-15555",
    "network": "udp",
    "process_name": "game.exe",
    "client_config_revision": "20260616.1",
    "capture_mode": "process"
  }
}
```

成功标准：

```text
1. OPEN_UDP 返回 flow_id。
2. 客户端发送 QUIC DATAGRAM。
3. 节点转发到 UDP 目标。
4. 目标响应后节点通过 QUIC DATAGRAM 回传。
5. 客户端按 flow_id 分发回原本游戏 UDP socket。
```

## 严格禁止

客户端实现必须避免：

- 未命中规则时默认发给节点。
- 忽略 `target_denied` 后换一个 rule_id 重试。
- 把用户可编辑的任意域名/IP 直接转发给节点。
- 把 `hmac_secret` 放进客户端。
- 在 token 过期后继续新建连接。
- UDP datagram 超过推荐大小后无限排队。
- TCP payload 在 `OPEN_TCP` 成功前写入 stream。
- 把 `process_name` 当作安全授权依据。

## 节点当前能力

当前节点已支持：

- QUIC 连接。
- HELLO / AUTH / AUTH_OK。
- PING / PONG。
- OPEN_TCP。
- OPEN_UDP。
- QUIC DATAGRAM UDP。
- token 的用户、设备、连接数、限速、TCP/UDP 权限。
- 解析 structured metadata。
- token claims 校验 `game_ids` / `policy_ids`。
- 节点本地 `route_policies` 校验。
- `/sessions` 输出 `game_id` / `policy_id` / `rule_id`。
- metrics 按游戏和策略聚合。

当节点未配置 `route_policies` 时，为兼容旧联调，节点不会强制要求 flow metadata；一旦配置 `route_policies`，客户端必须按本文完整 metadata 发送，否则 `OPEN_TCP` / `OPEN_UDP` 会返回 `target_denied`。
