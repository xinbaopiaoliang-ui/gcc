# gaccel QUIC 协议草案

## 连接

- 传输层：QUIC over UDP。
- ALPN：默认 `gaccel/1`。
- UDP 实时数据：QUIC DATAGRAM。
- TCP 转发：独立 QUIC stream。
- 控制消息：JSON Lines，每条 JSON 以换行结束。

## 控制消息

### HELLO

客户端发送：

```json
{"type":"HELLO","version":1}
```

服务端返回：

```json
{"type":"HELLO","version":1}
```

### AUTH

客户端发送：

```json
{"type":"AUTH","token":"dev-token"}
```

服务端返回：

```json
{"type":"AUTH_OK","version":1}
```

失败时：

```json
{"type":"ERROR","error_code":"auth_failed","error":"authentication failed"}
```

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
gaccel-token -secret "replace-with-a-long-random-secret" -user user-1 -device device-1 -ttl 15m
```

### OPEN_UDP

客户端通过控制 stream 请求创建 UDP flow：

```json
{"type":"OPEN_UDP","target_host":"8.8.8.8","target_port":53}
```

服务端返回：

```json
{"type":"OPEN_UDP","flow_id":1}
```

后续该 UDP flow 的数据通过 QUIC DATAGRAM 传输。

### OPEN_TCP

客户端打开一条新的 QUIC stream，并把第一条消息作为 TCP 打开请求：

```json
{"type":"OPEN_TCP","target_host":"example.com","target_port":443}
```

服务端返回：

```json
{"type":"OPEN_TCP","flow_id":2}
```

收到成功响应后，该 QUIC stream 切换为原始 TCP 字节流。客户端必须等待成功响应后再发送 TCP payload，避免 JSON decoder 预读后续原始字节。

### PING

```json
{"type":"PING"}
```

服务端返回：

```json
{"type":"PONG"}
```

### CLOSE_FLOW

```json
{"type":"CLOSE_FLOW","flow_id":1}
```

当前版本只显式关闭 UDP flow。TCP flow 会随对应 QUIC stream 关闭而释放。

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

建议客户端把 DATAGRAM payload 控制在 1200 字节以内，降低路径 MTU 导致的丢包风险。

## 服务端安全策略

服务端会拒绝：

- 未鉴权连接。
- 未授权端口。
- 私网、本机、链路本地、多播地址。
- 云元数据地址，例如 `169.254.169.254`。
- 超过节点最大连接数或单连接最大 flow 数的请求。

## 管理接口

默认监听 `127.0.0.1:9090`。

```text
GET /health
GET /status
GET /sessions
GET /metrics
POST /config/reload
GET /debug/pprof/
```

`/config/reload` 会重新读取启动时的配置文件路径。新鉴权、新连接、新 flow 会读取最新配置；监听地址、TLS 证书和 QUIC listener 级参数需要重启进程才能完全生效。

## 测试工具

`gaccel-probe` 仅用于协议验证，不是正式客户端。

PING：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode ping
```

UDP：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode udp -target-host 8.8.8.8 -target-port 53 -payload test -count 10
```

TCP：

```bash
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode tcp -target-host example.com -target-port 80 -payload "GET / HTTP/1.0\r\n\r\n"
```
