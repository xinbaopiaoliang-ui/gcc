# 客户端会话生命周期与断开判定

适用版本：`v0.6.20+`

这份文档说明客户端连接节点后，节点和控制面板如何记录“什么时候连接、什么时候认证成功、什么时候结束、为什么断开”。它给客户端和业务后台共同使用。

## 1. 基本结论

- 客户端连接节点使用 QUIC。
- 客户端 `HELLO` 后，节点会记录 `session_started`。
- 客户端 `AUTH` 成功后，节点会记录 `session_authenticated`。
- QUIC 连接关闭、客户端断开、网络中断、节点心跳超时或节点服务停止时，节点会记录 `session_ended`。
- 节点会通过 `POST /api/nodes/report` 把当前在线会话和会话事件上报到控制面板。
- 控制面板把会话写入 MySQL，表名为 `panel_client_sessions`，事件表为 `panel_client_session_events`。

## 2. 心跳与断开判定

节点在 `HELLO` 返回里会告诉客户端建议心跳间隔：

```json
{
  "type": "HELLO",
  "server": {
    "keepalive_interval_seconds": 15
  }
}
```

客户端应按 `keepalive_interval_seconds` 发送 `PING`。当前默认值是 `15s`。

节点侧新增配置：

```yaml
limits:
  heartbeat_interval: "15s"
  session_disconnect_timeout: "45s"
```

建议客户端规则：

- 正常在线时，每 `15s` 发送一次 `PING`。
- 如果本地网络切换、休眠唤醒、Steam 进程重启导致连接断开，应重新 Dial QUIC 并重新认证。
- 客户端主动退出时，尽量正常关闭 QUIC 连接。
- 如果客户端进程被强杀或后台直接关闭，节点无法收到主动关闭包，会在超过 `session_disconnect_timeout` 后判定为 `heartbeat_timeout`。

默认 `45s` 的含义：

- 客户端漏 1 次心跳不会立刻被踢。
- 连续大约 3 个心跳周期没有任何活跃信息，节点认为会话已经失联。
- 这个时间比游戏场景常见网络抖动宽松，但不会让离线会话长时间占用统计。

## 3. 断开原因

`close_reason` 目前可能出现：

| 值 | 含义 | 常见场景 |
| --- | --- | --- |
| `client_shutdown` | 客户端主动关闭 | 用户退出客户端、客户端重连前主动断开 |
| `heartbeat_timeout` | 心跳超时 | 客户端被强杀、电脑休眠、网络断开后没有正常关闭 |
| `quic_idle_timeout` | QUIC 空闲超时 | 长时间没有有效 QUIC 活动 |
| `network_lost` | 网络中断 | 连接异常关闭，但无法明确是客户端主动关闭 |
| `node_shutdown` | 节点服务停止 | 节点重启、升级、systemd 停止 |

`close_source` 目前可能出现：

| 值 | 含义 |
| --- | --- |
| `client` | 客户端侧主动结束 |
| `node` | 节点侧主动结束或服务停止 |
| `network` | 网络异常导致 |

## 4. 控制面板查询 API

### 面板页面接口

```http
GET /api/panel/client-sessions
Authorization: Bearer <panel_access_token>
```

查询参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `node_id` | 否 | 按节点 ID 过滤 |
| `user_id` | 否 | 按业务用户 ID 过滤 |
| `device_id` | 否 | 按设备 ID 过滤 |
| `status` | 否 | `online` 或 `closed` |
| `close_reason` | 否 | 例如 `heartbeat_timeout` |
| `window_hours` | 否 | 最近多少小时，默认 `24`，最大 `2160` |
| `limit` | 否 | 默认 `50`，最大 `500` |
| `offset` | 否 | 分页偏移 |

响应示例：

```json
{
  "status": "ok",
  "sessions": [
    {
      "node_id": "node-hk-01",
      "session_id": "26",
      "remote_addr": "123.234.174.226:52616",
      "user_id": "user-1",
      "device_id": "win-device-1",
      "client_id": "velox-gaccel",
      "client_version": "0.1.0",
      "client_platform": "windows/x86_64",
      "protocol_version": 1,
      "status": "closed",
      "close_reason": "heartbeat_timeout",
      "close_source": "node",
      "game_ids": ["steam"],
      "policy_ids": ["steam-web-v1"],
      "config_revision": "20260623.1",
      "connected_at": "2026-06-23T10:00:00+08:00",
      "authenticated_at": "2026-06-23T10:00:01+08:00",
      "last_seen_at": "2026-06-23T10:00:45+08:00",
      "ended_at": "2026-06-23T10:01:30+08:00",
      "duration_seconds": 90,
      "max_connections": 64,
      "rate_limit_mbps": 100,
      "allow_tcp": true,
      "allow_udp": true,
      "udp_flows": 0,
      "tcp_flows": 3,
      "udp_client_to_target_bytes": 0,
      "udp_target_to_client_bytes": 0,
      "tcp_client_to_target_bytes": 1024,
      "tcp_target_to_client_bytes": 4096
    }
  ],
  "overview": {
    "online_sessions": 3,
    "closed_sessions": 18,
    "timeout_sessions": 2,
    "total_sessions": 21,
    "total_duration_seconds": 3600,
    "udp_client_to_target_bytes": 1024,
    "udp_target_to_client_bytes": 2048,
    "tcp_client_to_target_bytes": 4096,
    "tcp_target_to_client_bytes": 8192
  },
  "limit": 50,
  "offset": 0,
  "count": 1
}
```

### 业务后台接口

```http
GET /api/backend/client-sessions
Authorization: Bearer <backend_api_key>
```

查询参数和响应字段与 `/api/panel/client-sessions` 相同。

业务后台可以用它做：

- 用户在线状态查询。
- 用户连接历史查询。
- 判断用户是否频繁 `heartbeat_timeout`。
- 对接客服排障，确认用户连接节点、认证成功和断开的时间。

## 5. 数据库表

升级需要执行：

```sql
source migrations/20260623_v0616_client_sessions.sql;
```

新增表：

- `panel_client_sessions`：每个节点会话的当前状态与最终状态。
- `panel_client_session_events`：节点上报的 start/auth/end 事件记录。

`panel_client_sessions` 是主要查询表，业务后台一般只需要读 API，不建议直接读数据库。

## 6. 客户端实现要求

客户端需要做到：

- `HELLO` 时传 `client_id`、`client_version`、`client_platform`。
- `AUTH` token 中必须包含稳定的 `user_id` 和 `device_id`。
- 按节点返回的 `keepalive_interval_seconds` 定时发送 `PING`。
- 发生断网或节点重启后，使用退避策略重新连接，不要毫秒级无限重连。
- 打开 TCP/UDP flow 时继续传 `game_id`、`policy_id`、`config_revision`，方便面板排查游戏维度问题。

客户端不需要直接上报“断开事件”给控制面板。断开判定以节点观测为准。
