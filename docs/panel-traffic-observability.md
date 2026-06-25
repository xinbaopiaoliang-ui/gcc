# 控制面板流量统计与联调观测

适用版本：v0.6.10 起。

## 目标

这个功能用于控制面板直接查看节点转发情况，主要解决三类问题：

- 客户端是否真的连上节点。
- 客户端是否产生 TCP/UDP 转发流量。
- 慢、断连、策略未生效时，先从面板看到哪个节点、用户、游戏策略或 flow reason 有异常。

## 数据来源

控制面板不直接抓包，也不连接客户端。统计来自节点定期上报：

```http
POST /api/nodes/report
```

节点上报的 `metrics` 已包含：

- `active_quic_connections`
- `active_tcp_flows`
- `active_udp_flows`
- `tcp_client_to_target_bytes`
- `tcp_target_to_client_bytes`
- `udp_client_to_target_bytes`
- `udp_target_to_client_bytes`
- `users[]`
- `flow_events[]`

控制面板从已有 `panel_node_reports.metrics_json` 计算统计，不新增 SQL 迁移。

## 面板接口

```http
GET /api/panel/traffic/overview?window_hours=24&limit=20
Authorization: Bearer <panel_jwt>
```

说明：

- 这是控制面板登录 JWT 接口，不使用 `backend_api_key`。
- `window_hours` 可选：`1`、`6`、`24`、`72`、`168`。
- `limit` 可选：排行返回数量，默认 20，最大 100。

## 响应字段

```json
{
  "status": "ok",
  "traffic": {
    "window_seconds": 86400,
    "sample_mode": "window_delta",
    "totals": {
      "node_count": 2,
      "online_node_count": 2,
      "report_node_count": 2,
      "active_quic_connections": 4,
      "active_tcp_flows": 16,
      "active_udp_flows": 8,
      "total_bytes": 12345678,
      "flow_open_errors": 0,
      "flow_close_events": 12,
      "policy_drift_nodes": 0
    },
    "nodes": [],
    "users": [],
    "flow_events": [],
    "policy_events": [],
    "policy_consistency": [],
    "recommendations": []
  }
}
```

`sample_mode` 含义：

| 值 | 含义 |
| --- | --- |
| `window_delta` | 窗口内至少有两个上报样本，流量按最新样本减最早样本计算。 |
| `latest_cumulative` | 窗口内只有单样本，无法计算窗口差值，暂按节点本次启动后的累计值显示。 |

## 页面使用

控制面板左侧进入“流量与联调观测”。

重点看这几块：

- “活跃 QUIC”：客户端是否正在连节点。
- “TCP / UDP Flow”：是否真的打开了转发流。
- “打开失败”：如果不为 0，看 Flow 事件排行里的 `reason`。
- “节点流量排行”：确认流量是否集中在某个节点。
- “用户流量排行”：确认某个 `user_id` 是否产生流量。
- “游戏/策略事件”：确认 `game_id`、`policy_id` 是否有命中。
- “策略一致性”：确认节点当前策略是否等于目标策略。

## 排障顺序

1. 活跃 QUIC 为 0：先查客户端是否连上节点，或节点 `journalctl -u gaccel-node -f` 是否有 `authenticated`。
2. 活跃 QUIC 大于 0，但 TCP/UDP Flow 为 0：查客户端是否真正按进程/域名打开 flow。
3. Flow 打开失败大于 0：看 `reason`，常见是策略拒绝、目标端口被安全策略拒绝、token 权限不足。
4. 策略漂移大于 0：先下发目标策略，或检查节点是否能拉取 `/api/nodes/commands`。
5. 有 flow 但访问慢：对比节点排行和用户排行，再结合目标线路、节点带宽、节点 UDP buffer、客户端本地网络排查。

## 部署注意

- 不需要新增 SQL。
- 线上已有 `panel_node_reports` 表即可使用。
- 页面统计依赖节点定期上报，如果节点 `panel report failed`，统计不会刷新。
- 这不是精确计费系统，当前用于运维观测和联调排障；后续如要做账单，需要增加独立的不可回退用量流水。
