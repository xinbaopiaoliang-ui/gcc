# 客户端对接与控制面板边界说明

适用对象：客户端开发、业务后台开发、联调负责人。

这份文档只说明客户端该拿什么、连哪里、发什么。控制面板是运维系统，不是客户端接口。

## 一、总链路

```text
业务后台
  1. 判断用户套餐、游戏权限、节点可用性
  2. 读取该节点 hmac_secret 明文
  3. 签发短期 JWT token
  4. 返回客户端配置

客户端
  1. 从业务后台拿配置
  2. 本地按进程、域名、IP、端口命中规则
  3. 直接连接节点 endpoint_host:endpoint_port
  4. QUIC HELLO -> AUTH -> OPEN_TCP / OPEN_UDP

节点
  1. 用本机 hmac_secret 验证 token
  2. 校验 token 权限、flow metadata 和基础安全边界
  3. 按客户端已判定的目标转发 TCP/UDP

控制面板
  1. 管节点、部署、更新、策略下发
  2. 接收节点上报和展示流量
  3. 不参与客户端数据转发
```

## 二、客户端不能使用的东西

客户端不要调用这些接口，也不要保存这些密钥：

| 内容 | 原因 |
| --- | --- |
| `http://103.201.131.99:8091/api/panel/*` | 面板登录接口，只给浏览器管理后台使用 |
| `http://103.201.131.99:8091/api/backend/*` | 业务后台同步控制面板使用，需要 `backend_api_key` |
| `backend_api_key` | 只给业务后台调用控制面板 |
| `hmac_secret` | 只给业务后台签 token、节点验 token |
| `admin_host/admin_port` | 控制面板诊断节点用，不是客户端入口 |
| `ssh_host/ssh_port/ssh_user` | 控制面板部署更新用，不是客户端入口 |

客户端只需要业务后台返回的客户端配置和短期 token。

## 三、业务后台给客户端的建议接口

这个接口由业务后台实现，不是控制面板提供。

```http
POST /api/client/accelerate/session
Authorization: Bearer <用户登录态>
Content-Type: application/json
```

请求示例：

```json
{
  "device_id": "win-device-01",
  "client_version": "0.1.0",
  "client_platform": "windows/amd64",
  "game_id": "steam",
  "region": "hk"
}
```

响应示例：

```json
{
  "revision": "20260624.1",
  "expires_at": "2026-06-24T18:00:00+08:00",
  "user_id": "user-10001",
  "device_id": "win-device-01",
  "nodes": [
    {
      "node_id": "hk-01",
      "name": "香港 01",
      "endpoint_host": "47.83.160.126",
      "endpoint_port": 6666,
      "alpn": "gaccel/1",
      "sni": "",
      "insecure": false,
      "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
      "token_type": "Bearer",
      "token_expires_at": "2026-06-24T18:00:00+08:00",
      "max_connections": 64,
      "rate_limit_mbps": 100,
      "allow_tcp": true,
      "allow_udp": true,
      "policy_ids": ["steam-web-v1"]
    }
  ],
  "games": [
    {
      "game_id": "steam",
      "name": "Steam",
      "processes": [
        {"process_name": "steam.exe"},
        {"process_name": "steamwebhelper.exe"}
      ],
      "policies": [
        {
          "policy_id": "steam-web-v1",
          "rules": [
            {
              "rule_id": "steam-store-https",
              "network": "tcp",
              "target_type": "domain",
              "target_value": "store.steampowered.com",
              "port_start": 443,
              "port_end": 443,
              "action": "quic_relay"
            },
            {
              "rule_id": "steam-community-https",
              "network": "tcp",
              "target_type": "domain_suffix",
              "target_value": ".steamcommunity.com",
              "port_start": 443,
              "port_end": 443,
              "action": "quic_relay"
            }
          ]
        }
      ]
    }
  ]
}
```

字段说明：

| 字段 | 客户端用途 |
| --- | --- |
| `revision` | 客户端配置版本，后续放入 `metadata.client_config_revision` |
| `nodes[].endpoint_host` | 客户端连接节点的公网 IP 或域名 |
| `nodes[].endpoint_port` | 客户端连接节点的 QUIC UDP 端口 |
| `nodes[].alpn` | QUIC ALPN，当前为 `gaccel/1` |
| `nodes[].token` | 连接该节点用的短期 JWT |
| `games[].processes` | 客户端本机进程匹配 |
| `games[].policies[].rules` | 客户端命中后决定是否打开 TCP/UDP flow |

## 四、客户端连接节点流程

### 1. 建立 QUIC

客户端连接：

```text
nodes[].endpoint_host:nodes[].endpoint_port
```

传输层：

| 项 | 值 |
| --- | --- |
| 协议 | QUIC over UDP |
| ALPN | `gaccel/1` |
| TLS | TLS 1.3 |
| 控制消息 | JSON Lines，每条 JSON 后带 `\n` |
| TCP | 每个 TCP flow 使用独立 QUIC bidirectional stream |
| UDP | `OPEN_UDP` 创建 flow，payload 走 QUIC DATAGRAM |

### 2. HELLO

```json
{
  "type": "HELLO",
  "version": 1,
  "client_id": "win-device-01-session-1",
  "client_version": "0.1.0",
  "client_platform": "windows/amd64"
}
```

节点返回：

```json
{
  "type": "HELLO",
  "version": 1,
  "server": {
    "alpn": "gaccel/1",
    "protocol_version": 1,
    "capabilities": ["auth_hmac", "udp_datagram", "tcp_stream", "ping", "flow_close_notify", "flow_metadata", "route_policy"],
    "keepalive_interval_seconds": 15,
    "datagram_header_bytes": 11,
    "recommended_datagram_bytes": 1200,
    "recommended_datagram_payload_bytes": 1189,
    "token_policy": "validated_on_auth"
  }
}
```

### 3. AUTH

```json
{
  "type": "AUTH",
  "token": "<nodes[].token>"
}
```

成功返回：

```json
{
  "type": "AUTH_OK",
  "user_id": "user-10001",
  "device_id": "win-device-01"
}
```

## 五、OPEN_TCP

客户端命中 TCP 规则后，新开一条 QUIC bidirectional stream，第一条消息发送：

```json
{
  "type": "OPEN_TCP",
  "target_host": "store.steampowered.com",
  "target_port": 443,
  "metadata": {
    "game_id": "steam",
    "policy_id": "steam-web-v1",
    "rule_id": "steam-store-https",
    "network": "tcp",
    "process_name": "steamwebhelper.exe",
    "client_config_revision": "20260624.1",
    "capture_mode": "process",
    "trace_id": "client-generated-trace-id"
  }
}
```

成功返回：

```json
{
  "type": "OPEN_TCP",
  "flow_id": 1
}
```

收到成功响应之前，客户端不能写入 TCP payload。成功后，这条 QUIC stream 切换成原始 TCP 字节流。

## 六、OPEN_UDP

客户端命中 UDP 规则后，在控制流发送：

```json
{
  "type": "OPEN_UDP",
  "target_host": "203.0.113.10",
  "target_port": 27015,
  "metadata": {
    "game_id": "example_game",
    "policy_id": "example-game-realtime-v1",
    "rule_id": "example-game-udp-27015",
    "network": "udp",
    "process_name": "game.exe",
    "client_config_revision": "20260624.1",
    "capture_mode": "process",
    "trace_id": "client-generated-trace-id"
  }
}
```

成功返回：

```json
{
  "type": "OPEN_UDP",
  "flow_id": 2
}
```

之后 UDP payload 使用 QUIC DATAGRAM。单个 datagram payload 不要超过节点返回的 `recommended_datagram_payload_bytes`。

## 七、metadata 必填规则

当前节点默认使用 `route_policies.mode: "client_decision"`。

含义是：业务后台把游戏规则下发给客户端，客户端本机完成进程、域名、IP、端口和协议的命中判断；节点只验证 token 是否允许该 `game_id`、`policy_id` 和 `client_config_revision`，并继续执行私网、危险端口、限流等基础安全校验。节点不再根据本地策略逐条匹配 `target_host`、`target_port` 和 `rule_id`。

当 token 带有 `game_ids`、`policy_ids` 或 `config_revision` 限制时，`OPEN_TCP` 和 `OPEN_UDP` 必须带 metadata。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `game_id` | 是 | 必须在 token `game_ids` 里 |
| `policy_id` | 是 | 必须在 token `policy_ids` 里 |
| `rule_id` | 建议 | 客户端命中的规则 ID，用于日志、统计和问题排查；当前默认模式不靠它放行 |
| `network` | 是 | `tcp` 或 `udp`，必须和 OPEN 类型一致 |
| `client_config_revision` | 是 | 必须和 token `config_revision` 一致，除非 token 未写该字段 |
| `process_name` | 建议 | 只用于日志和排查，不作为节点放行依据 |
| `process_path_hash` | 可选 | 用于排查或反作弊辅助 |
| `capture_mode` | 建议 | 例如 `process`、`wfp`、`windivert`、`tun` |
| `trace_id` | 可选 | 客户端生成，便于链路排查 |

## 八、token claims 要求

token 由业务后台签发，客户端只保存和发送，不需要知道签名密钥。

```json
{
  "sub": "user-10001",
  "user_id": "user-10001",
  "device_id": "win-device-01",
  "exp": 1782115200,
  "nbf": 1782111600,
  "iat": 1782111600,
  "max_connections": 64,
  "rate_limit_mbps": 100,
  "allow_tcp": true,
  "allow_udp": true,
  "game_ids": ["steam"],
  "policy_ids": ["steam-web-v1"],
  "config_revision": "20260624.1"
}
```

客户端需要关注：

- `exp` 过期后重新向业务后台拿 token。
- `max_connections` 不是无限连接，超过会被节点拒绝。
- `allow_tcp=false` 时不要发 `OPEN_TCP`。
- `allow_udp=false` 时不要发 `OPEN_UDP`。
- `config_revision` 要和 `metadata.client_config_revision` 对齐。

## 九、错误码处理

节点返回错误时格式如下：

```json
{
  "type": "ERROR",
  "error_code": "target_denied",
  "error": "route policy denied: ..."
}
```

客户端必须按 `error_code` 处理，不要依赖 `error` 文本。

| error_code | 客户端处理 |
| --- | --- |
| `token_expired` | 重新向业务后台申请 token |
| `token_not_active` | 校准时间或稍后重试 |
| `token_missing_exp` | token 无效，重新申请 |
| `token_invalid` | token 验签失败，重新申请；如果仍失败，业务后台检查节点 hmac_secret |
| `unauthorized` | 先完成 AUTH |
| `permission_denied` | token 没有 TCP/UDP 权限 |
| `target_denied` | token 授权不匹配、metadata 缺失/错误、目标命中节点安全边界或危险端口 |
| `max_flows_exceeded` | 减少并发 flow，稍后重试 |
| `max_connections_exceeded` | 当前 token 连接数超限，关闭旧连接或重新申请 |
| `open_tcp_failed` | TCP 目标打开失败，可对同一规则做有限重试 |
| `open_udp_failed` | UDP 目标打开失败，可对同一规则做有限重试 |

## 十、联调验收标准

客户端联调成功至少满足：

1. 能从业务后台拿到 `revision`、`nodes[]`、`games[]` 和短期 token。
2. 能直连节点 QUIC，并完成 `HELLO`、`AUTH`。
3. Steam 商店、社区、论坛这类 TCP 目标能通过 `OPEN_TCP` 正常访问。
4. 游戏 UDP 目标能通过 `OPEN_UDP` + QUIC DATAGRAM 收发。
5. 控制面板能看到客户端会话、流量、flow 事件和断开记录。
6. 客户端没有访问控制面板 API，没有保存 `hmac_secret`。

## 十一、排查分工

| 问题 | 优先看哪里 |
| --- | --- |
| 客户端拿不到配置 | 业务后台接口日志 |
| `token_invalid` | 业务后台签名密钥是否和节点 `hmac_secret` 一致 |
| `target_denied` | 先查 token 的 `game_ids/policy_ids/config_revision` 和 OPEN metadata 是否一致，再查目标是否被节点基础安全策略拦截 |
| 连接节点失败 | 节点公网 UDP 端口、防火墙、QUIC ALPN、证书配置 |
| 打开失败次数上涨 | 控制面板“流量与联调观测”里的 Flow 事件排行 |
| 已连接但无流量 | 客户端本地进程/规则是否命中，是否真的发起 `OPEN_TCP` / `OPEN_UDP` |
