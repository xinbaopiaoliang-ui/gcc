# Rust 客户端联调指南

本文档给 Rust 客户端开发者使用，目标是把已验证的 Go demo 流程翻译成 Rust 侧实现步骤。

当前范围只描述客户端如何直连节点协议，不描述 SOCKS5、TUN、VPN、系统代理或驱动层接管。

## 目标链路

Steam 社区 HTTPS 联调链路：

```text
Rust 客户端
  -> Token API 获取短期 JWT
  -> QUIC 连接节点
  -> Control Stream: HELLO / AUTH / PING
  -> QUIC Stream: OPEN_TCP steamcommunity.com:443
  -> 在该 QUIC Stream 上跑 TLS
  -> HTTPS GET /
```

UDP 游戏流量联调链路：

```text
Rust 客户端
  -> QUIC 连接节点并鉴权
  -> Control Stream: OPEN_UDP 目标地址
  -> QUIC DATAGRAM: UDP payload
```

## 推荐 Rust 依赖

建议先用主流 async 栈：

```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
quinn = "0.11"
rustls = "0.23"
tokio-rustls = "0.26"
webpki-roots = "1"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
reqwest = { version = "0.12", features = ["json", "rustls-tls"] }
bytes = "1"
thiserror = "2"
tracing = "0.1"
```

版本以客户端项目的 `Cargo.lock` 为准。升级 `quinn` / `rustls` 时，先跑 Steam 社区 HTTPS 和 UDP echo 两条回归测试。

参考资料：

- `quinn`：QUIC endpoint、stream、datagram 实现。
- `tokio-rustls`：在异步 stream 上跑 TLS。
- `serde_json`：控制消息 JSON 编解码。

## 配置结构

客户端建议先维护一个本地配置结构：

```rust
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct GaccelClientConfig {
    pub node_addr: String,
    pub alpn: String,
    pub sni: Option<String>,
    pub insecure_skip_verify: bool,

    pub token_api_url: Option<String>,
    pub token_api_key: Option<String>,
    pub token: Option<String>,
    pub user_id: String,
    pub device_id: String,
    pub ttl_seconds: u64,

    pub client_id: String,
    pub client_version: String,
    pub client_platform: String,
}
```

Steam 社区联调默认值：

```text
node_addr: 195.245.242.9:5555
alpn: gaccel/1
target_host: steamcommunity.com
target_port: 443
http_path: /
```

## Token 获取

Token API 是后端/面板接口，不建议直接暴露给未登录用户。正式产品里，Rust 客户端通常应该向你的业务后端请求 token，由业务后端再调用 `gaccel-token-api`。

联调阶段可以直接请求 Token API：

```http
POST /token
Authorization: Bearer <token-api-key>
Content-Type: application/json

{
  "user_id": "dev",
  "device_id": "steam-client-test",
  "ttl_seconds": 3600,
  "allow_tcp": true,
  "allow_udp": true
}
```

返回：

```json
{
  "token": "eyJ...",
  "token_type": "Bearer",
  "user_id": "dev",
  "device_id": "steam-client-test",
  "expires_at": "2026-06-16T11:57:31Z",
  "expires_in_seconds": 3600
}
```

Rust 侧只保存并使用 `token` 字段。不要把 `hmac_secret` 放进客户端。

## QUIC 连接

必须项：

- QUIC over UDP。
- ALPN 必须是 `gaccel/1`。
- 协议版本当前是 `1`。
- 如果节点使用自签证书，联调时可临时跳过节点证书校验；生产环境应使用可信证书或证书固定。
- UDP relay 需要 QUIC DATAGRAM 能力。

连接成功后先打开一条双向 stream 作为 control stream。

## 控制消息结构

建议 Rust 侧定义和服务端一致的消息结构：

```rust
#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct ControlMessage {
    #[serde(rename = "type")]
    pub message_type: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub version: Option<u8>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub flow_id: Option<u32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub target_host: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub target_port: Option<u16>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub client_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub client_version: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub client_platform: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub user_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub device_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error_code: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub server: Option<serde_json::Value>,
}
```

控制消息是 JSON Lines：每条 JSON 后必须追加 `\n`。

```rust
async fn write_json_line<W: tokio::io::AsyncWrite + Unpin>(
    writer: &mut W,
    msg: &ControlMessage,
) -> anyhow::Result<()> {
    let mut data = serde_json::to_vec(msg)?;
    data.push(b'\n');
    tokio::io::AsyncWriteExt::write_all(writer, &data).await?;
    tokio::io::AsyncWriteExt::flush(writer).await?;
    Ok(())
}
```

读取时建议手动读到 `\n`：

```rust
async fn read_json_line<R: tokio::io::AsyncRead + Unpin>(
    reader: &mut R,
) -> anyhow::Result<ControlMessage> {
    let mut line = Vec::with_capacity(512);
    loop {
        let mut byte = [0u8; 1];
        let n = tokio::io::AsyncReadExt::read(reader, &mut byte).await?;
        if n == 0 {
            anyhow::bail!("stream closed while reading control line");
        }
        line.push(byte[0]);
        if byte[0] == b'\n' {
            break;
        }
        if line.len() > 64 * 1024 {
            anyhow::bail!("control line too large");
        }
    }
    Ok(serde_json::from_slice(&line)?)
}
```

不要在 `OPEN_TCP` stream 上使用可能预读后续原始 TCP 字节的 JSON decoder。必须只读第一行 JSON 响应，然后把剩余 stream 交给 TLS 或业务协议。

## 握手流程

1. 打开 control stream。
2. 发送 `HELLO`：

```json
{
  "type": "HELLO",
  "version": 1,
  "client_id": "steam-client-test",
  "client_version": "0.1.0",
  "client_platform": "windows/amd64"
}
```

3. 读取服务端 `HELLO`。
4. 发送 `AUTH`：

```json
{
  "type": "AUTH",
  "version": 1,
  "token": "eyJ...",
  "client_id": "steam-client-test",
  "client_version": "0.1.0",
  "client_platform": "windows/amd64"
}
```

5. 读取 `AUTH_OK`。
6. 开始保活和业务 flow。

`AUTH_OK` 会返回 `user_id`、`device_id`，建议客户端记录到日志里，方便和节点 `/sessions` 对账。

## 保活和重连

建议：

- 每 15 秒发送一次 `PING`。
- 单次 `PING` 超过 5 秒未收到 `PONG`，记一次失败。
- 连续 2-3 次失败，关闭 QUIC 连接并重连。
- 重连时必须重新走 `HELLO` / `AUTH`。
- 如果 token 剩余有效期小于 60 秒，先刷新 token 再重连。

`PING`：

```json
{"type":"PING"}
```

`PONG`：

```json
{"type":"PONG"}
```

## TCP: Steam 社区 HTTPS

每个 TCP flow 使用一条新的 QUIC 双向 stream。

步骤：

1. `connection.open_bi()` 打开新 stream。
2. 写入一条 `OPEN_TCP` JSON Line：

```json
{"type":"OPEN_TCP","target_host":"steamcommunity.com","target_port":443}
```

3. 读取一条 JSON Line。
4. 如果返回 `OPEN_TCP` 且带 `flow_id`，这条 QUIC stream 立即切换为原始 TCP 字节流。
5. 用 `tokio-rustls` 在这条 stream 上做 TLS，SNI 使用 `steamcommunity.com`。
6. 在 TLS stream 上发送 HTTP 请求：

```http
GET / HTTP/1.1
Host: steamcommunity.com
User-Agent: gaccel-rust-client/0.1.0
Accept: */*
Connection: close
```

7. 读取 HTTP 响应，联调成功标志：

```text
HTTP/1.1 200 OK
<title>Steam Community</title>
```

客户端实际接入 Steam 社区模块时，不一定要自己构造 HTTP 请求；可以把目标协议流量写入 TLS stream。关键是：节点只看到 `OPEN_TCP` 后的字节流，不理解也不修改 HTTPS 内容。

## UDP: 游戏数据

先通过 control stream 打开 UDP flow：

```json
{"type":"OPEN_UDP","target_host":"103.201.131.99","target_port":15555}
```

成功：

```json
{"type":"OPEN_UDP","flow_id":1}
```

后续使用 QUIC DATAGRAM。

Datagram 头部：

```text
version    1 byte, 当前 1
type       1 byte, UDP payload = 1
flow_id    4 bytes, big endian
seq        4 bytes, big endian
flags      1 byte, 当前 0
payload    n bytes
```

Rust 编码：

```rust
fn encode_udp_datagram(flow_id: u32, seq: u32, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(11 + payload.len());
    out.push(1); // version
    out.push(1); // datagram type: UDP payload
    out.extend_from_slice(&flow_id.to_be_bytes());
    out.extend_from_slice(&seq.to_be_bytes());
    out.push(0); // flags
    out.extend_from_slice(payload);
    out
}
```

解码时先检查长度至少 11 字节，再检查 `version == 1`、`type == 1`。

单个 QUIC datagram 建议总长不超过服务端 `HELLO.server.recommended_datagram_bytes`，当前通常为 `1200`。payload 建议不超过 `1189`。

## 错误处理

服务端错误格式：

```json
{"type":"ERROR","error_code":"token_expired","error":"token expired"}
```

客户端必须按 `error_code` 分支处理，不要依赖 `error` 文本。

建议处理：

```text
token_expired / token_not_active / token_missing_exp / token_invalid
  -> 刷新 token 后重新连接。

auth_failed / unauthorized
  -> 停止重试，提示登录态或后端签发异常。

max_connections_exceeded
  -> 关闭旧连接或等待后重试。

permission_denied
  -> 当前 token 不允许 TCP 或 UDP。

target_denied
  -> 目标地址或端口被节点安全策略拒绝。

rate_limited
  -> 降低发送速率，尤其是 UDP datagram。

max_flows_exceeded
  -> 关闭闲置 flow 或新建连接。

open_tcp_failed / open_udp_failed
  -> 目标不可达或上游拨号失败，可对单个目标重试。
```

## 状态机建议

```text
Idle
  -> FetchToken
  -> DialQuic
  -> Hello
  -> Auth
  -> Ready
  -> OpenTcp / OpenUdp
  -> Ready

任意状态遇到连接断开：
  -> Backoff
  -> FetchToken 或 DialQuic

任意状态遇到 token 错误：
  -> FetchToken
  -> DialQuic
```

退避建议：

```text
首次重试: 300ms
第二次: 800ms
第三次: 1500ms
之后: 3000-5000ms 加随机抖动
```

不要无限快速重连，避免对节点形成压力。

## 客户端日志字段

建议每条连接日志带这些字段：

```text
node_addr
client_id
client_version
client_platform
user_id
device_id
protocol_version
flow_id
network
target_host
target_port
latency_ms
error_code
```

这些字段可以和节点 `/sessions`、`/status`、`flow_events` 对齐。

## Steam 社区最小验收

客户端第一轮联调通过标准：

```text
1. Token 获取成功，返回 eyJ... JWT。
2. QUIC 连接节点成功。
3. HELLO 返回 server.capabilities。
4. AUTH 返回 AUTH_OK。
5. /sessions 能看到 client_id、client_version、client_platform。
6. OPEN_TCP steamcommunity.com:443 成功。
7. TLS 握手成功，SNI 是 steamcommunity.com。
8. HTTP GET / 返回 200 OK。
9. 响应 HTML 包含 <title>Steam Community</title>。
10. 断开后 /sessions 不残留 active flow。
```

UDP 第二轮联调通过标准：

```text
1. OPEN_UDP 到 UDP echo 服务成功。
2. QUIC DATAGRAM 发出 payload。
3. 收到同 flow_id 的响应 datagram。
4. 多包测试能统计 avg/min/max latency。
5. 超过 MTU 或限速时客户端不会阻塞主循环。
```

## 和当前 Go demo 的对应关系

可参考：

```text
cmd/gaccel-probe        命令行协议探针
cmd/gaccel-steam-demo   页面控制版联调 demo
cmd/gaccel-connect-demo HTTP CONNECT 到 QUIC 的真实页面流量联调 demo
docs/protocol.md        完整协议说明
docs/token-api.md       token 获取接口
```

Rust 客户端实现时，不需要复刻 Go 项目结构；只需要保持线协议兼容。
