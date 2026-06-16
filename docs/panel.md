# 面板对接协议

本文档描述节点和面板之间的最小对接方式。

当前设计原则：

- 节点主动访问面板，管理 API 不暴露公网。
- heartbeat/report 只上报状态，不做下发。
- 运维命令由节点主动拉取，面板响应必须带 HMAC 签名。
- 第一版命令支持 `noop`、`config_reload` 和 `apply_config`。

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

## 安全建议

- `panel.api_key` 和 `panel.command_secret` 必须是不同随机值。
- `command_secret` 不要下发给客户端。
- 面板命令接口必须走 HTTPS。
- 命令必须设置较短 `expires_at`。
- 面板侧需要记录命令 ID、操作者、目标节点、签名时间和执行结果。
- 配置包必须带 SHA256；升级包下发后续还需要包级 SHA256 或签名校验。
