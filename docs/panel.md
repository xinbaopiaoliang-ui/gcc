# 面板对接协议

本文档描述节点和面板之间的最小对接方式。

当前设计原则：

- 节点主动访问面板，管理 API 不暴露公网。
- heartbeat/report 只上报状态，不做下发。
- 运维命令由节点主动拉取，面板响应必须带 HMAC 签名。
- 第一版命令只支持 `noop` 和 `config_reload`。

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
  }
}
```

面板返回任意 `2xx` 即认为成功。非 `2xx` 或请求失败会记录 warning 日志，下个周期继续上报。

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

## 安全建议

- `panel.api_key` 和 `panel.command_secret` 必须是不同随机值。
- `command_secret` 不要下发给客户端。
- 面板命令接口必须走 HTTPS。
- 命令必须设置较短 `expires_at`。
- 面板侧需要记录命令 ID、操作者、目标节点、签名时间和执行结果。
- 后续配置包/升级包下发必须再做包级 SHA256 或签名校验。
