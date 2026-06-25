# 业务后台游戏配置与节点策略设计

本文档给业务后台、客户端和节点三方对齐使用。

目标不是只支持 Steam，而是支持任意 Windows 游戏的 TCP/UDP 加速。Steam 只是一个配置示例。

## 核心边界

业务后台负责：

- 维护游戏、进程、TCP/UDP 规则、节点、用户授权。
- 生成客户端配置快照。
- 生成节点策略快照。
- 签发短期 token 或调用 `gaccel-token-api` 签发 token。

客户端负责：

- 按本机进程识别流量。
- 按游戏规则判断 TCP/UDP 是否需要加速。
- 命中规则后通过 QUIC 连接节点。
- `OPEN_TCP` / `OPEN_UDP` 时带上 `game_id`、`policy_id`、`rule_id` 等元信息。

节点负责：

- 校验 token。
- 校验 `game_id` / `policy_id` / `rule_id` / `network` / `target_host` / `target_port` 是否允许。
- 通过校验后执行 TCP/UDP 转发。
- 记录流量、用户、游戏、策略、flow 事件。

重要原则：

- 节点不能按进程判断，进程只存在客户端本机。
- 客户端不能把任意规则直接下发给节点。
- 节点只信任业务后台同步的节点策略快照。
- 客户端发来的 `process_name` 只用于日志和排查，不能作为唯一安全依据。
- 业务后台、控制面板和节点之间统一用 `node_id` 识别节点；节点 IP/端口只作为客户端连接入口，可以变化。

## 总体链路

```text
业务后台
  -> 生成客户端配置
  -> 生成节点策略配置
  -> 签发短期 token
  -> 通过控制面板按 node_id 同步节点和 desired_policy_revision

客户端
  -> 按进程/域名/IP/端口/协议命中游戏规则
  -> OPEN_TCP / OPEN_UDP 带 metadata
  -> QUIC 连接节点

节点
  -> 校验 token 授权
  -> 校验本地节点策略
  -> TCP Stream Relay / UDP Datagram Relay
```

## 控制面板同步字段

业务后台同步节点到控制面板时，至少需要给：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `node_id` | 是 | 节点唯一 ID，作为业务后台、控制面板、节点配置三方主键。 |
| `name` | 是 | 节点展示名。 |
| `endpoint_host` | 是 | 客户端连接 QUIC 的公网 IP 或域名。 |
| `endpoint_port` | 是 | 客户端连接 QUIC 端口。 |
| `alpn` | 是 | 默认 `gaccel/1`。 |
| `admin_host` / `admin_port` | 建议 | 控制面板接入自检使用。远程节点若填 `127.0.0.1`，面板无法跨服务器直接探测。 |
| `allow_tcp` / `allow_udp` | 是 | 节点是否允许 TCP/UDP 转发。全平台游戏两者都建议支持。 |
| `desired_version` | 可选 | 期望节点版本，用于一键更新和漂移提示。 |
| `desired_policy_revision` | 可选 | 期望策略版本，用于策略同步闭环。 |

业务后台保存游戏和规则后，应生成 `route_policies`，调用控制面板的策略校验接口，通过后保存策略，再把对应 `revision` 设置为目标节点的 `desired_policy_revision`。

## 客户端传给节点的字段

### OPEN_TCP

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

### OPEN_UDP

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

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `target_host` | 是 | 客户端实际要访问的域名或 IP。 |
| `target_port` | 是 | 客户端实际要访问的端口。 |
| `metadata.game_id` | 是 | 游戏 ID，例如 `steam`、`pubg`、`lol`。 |
| `metadata.policy_id` | 是 | 后台生成的策略 ID，节点按它查规则。 |
| `metadata.rule_id` | 是 | 命中的具体规则 ID，便于节点快速校验和记录。 |
| `metadata.network` | 是 | `tcp` 或 `udp`，必须和消息类型一致。 |
| `metadata.process_name` | 建议 | 客户端本机进程名，只用于日志和排查。 |
| `metadata.process_path_hash` | 可选 | 进程路径 hash，辅助反作弊或排查。 |
| `metadata.client_config_revision` | 是 | 客户端配置版本，用于灰度和排查。 |
| `metadata.capture_mode` | 建议 | `process`、`wfp`、`windivert`、`tun` 等。 |
| `metadata.trace_id` | 可选 | 客户端生成的链路追踪 ID。 |

## token 建议 Claims

token 应携带游戏和策略授权字段：

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

节点校验顺序建议：

```text
1. token 是否有效。
2. token 是否允许当前 game_id。
3. token 是否允许当前 policy_id。
4. token 是否允许 network: tcp/udp。
5. 节点本地策略是否存在 policy_id。
6. rule_id 是否属于 policy_id。
7. target_host / target_port 是否命中 rule。
8. 通过后才创建 flow。
```

## MySQL 表结构

下面是业务后台初版表结构，面向 MySQL 8.0。

### 节点表

```sql
CREATE TABLE accel_nodes (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  node_id VARCHAR(64) NOT NULL UNIQUE COMMENT '节点唯一ID，例如 node-test-01',
  name VARCHAR(128) NOT NULL COMMENT '节点展示名',
  region VARCHAR(64) NOT NULL DEFAULT '' COMMENT '区域，例如 hk、jp、us-west',
  country VARCHAR(32) NOT NULL DEFAULT '' COMMENT '国家或地区',
  provider VARCHAR(64) NOT NULL DEFAULT '' COMMENT '供应商',
  line_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '线路类型，例如 premium、standard',
  endpoint_host VARCHAR(255) NOT NULL COMMENT '节点公网IP或域名',
  endpoint_port INT NOT NULL COMMENT 'QUIC端口',
  alpn VARCHAR(64) NOT NULL DEFAULT 'gaccel/1',
  sni VARCHAR(255) NOT NULL DEFAULT '',
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1,
  allow_udp TINYINT(1) NOT NULL DEFAULT 1,
  max_connections_per_user INT NOT NULL DEFAULT 2,
  rate_limit_mbps INT NOT NULL DEFAULT 50,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_region (region),
  INDEX idx_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='加速节点';
```

### 节点标签表

```sql
CREATE TABLE accel_node_tags (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  node_id VARCHAR(64) NOT NULL,
  tag VARCHAR(64) NOT NULL,
  UNIQUE KEY uk_node_tag (node_id, tag),
  INDEX idx_tag (tag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='节点标签';
```

### 游戏表

```sql
CREATE TABLE accel_games (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  game_id VARCHAR(64) NOT NULL UNIQUE COMMENT '游戏ID，例如 steam、pubg、lol',
  name VARCHAR(128) NOT NULL COMMENT '游戏展示名',
  platform VARCHAR(32) NOT NULL DEFAULT 'windows',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='游戏';
```

### 游戏进程表

```sql
CREATE TABLE accel_game_processes (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  game_id VARCHAR(64) NOT NULL,
  process_name VARCHAR(255) NOT NULL COMMENT '例如 steam.exe、game.exe',
  process_path_pattern VARCHAR(512) NOT NULL DEFAULT '' COMMENT '可选，路径匹配',
  process_path_hash CHAR(64) NOT NULL DEFAULT '' COMMENT '可选，进程路径hash',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_game_process (game_id, process_name, process_path_hash),
  INDEX idx_game_id (game_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='游戏进程匹配规则';
```

### 策略表

```sql
CREATE TABLE accel_policies (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  policy_id VARCHAR(96) NOT NULL UNIQUE COMMENT '策略ID，例如 steam-web-v1',
  game_id VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL,
  description VARCHAR(512) NOT NULL DEFAULT '',
  config_revision VARCHAR(64) NOT NULL DEFAULT '',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_game_id (game_id),
  INDEX idx_revision (config_revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='游戏加速策略';
```

### 策略规则表

TCP 和 UDP 都放在同一张规则表，通过 `network` 区分。

```sql
CREATE TABLE accel_policy_rules (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  rule_id VARCHAR(128) NOT NULL UNIQUE COMMENT '客户端传给节点的rule_id',
  policy_id VARCHAR(96) NOT NULL,
  game_id VARCHAR(64) NOT NULL,
  network ENUM('tcp','udp') NOT NULL,
  target_type ENUM('domain','domain_suffix','ip','cidr','any') NOT NULL,
  target_value VARCHAR(255) NOT NULL COMMENT '域名、后缀、IP、CIDR或*',
  port_start INT NOT NULL,
  port_end INT NOT NULL,
  action ENUM('quic_relay','direct','block') NOT NULL DEFAULT 'quic_relay',
  priority INT NOT NULL DEFAULT 100,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_policy_network (policy_id, network),
  INDEX idx_game_id (game_id),
  INDEX idx_target (target_type, target_value),
  INDEX idx_ports (port_start, port_end),
  INDEX idx_priority (priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='策略路由规则';
```

匹配说明：

| target_type | target_value 示例 | 说明 |
| --- | --- | --- |
| `domain` | `store.steampowered.com` | 精确域名。 |
| `domain_suffix` | `.steamcommunity.com` | 后缀匹配，包含子域名。 |
| `ip` | `203.0.113.10` | 精确 IP。 |
| `cidr` | `203.0.113.0/24` | IP 网段。 |
| `any` | `*` | 任意目标，必须谨慎使用，建议只配合端口范围和用户授权。 |

### 策略节点绑定表

```sql
CREATE TABLE accel_policy_nodes (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  policy_id VARCHAR(96) NOT NULL,
  node_id VARCHAR(64) NOT NULL,
  priority INT NOT NULL DEFAULT 100,
  weight INT NOT NULL DEFAULT 100,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_policy_node (policy_id, node_id),
  INDEX idx_node_id (node_id),
  INDEX idx_policy_id (policy_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='策略可用节点';
```

### 用户策略授权表

```sql
CREATE TABLE accel_user_policy_grants (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  user_id VARCHAR(64) NOT NULL,
  policy_id VARCHAR(96) NOT NULL,
  max_connections INT NOT NULL DEFAULT 2,
  rate_limit_mbps INT NOT NULL DEFAULT 50,
  allow_tcp TINYINT(1) NOT NULL DEFAULT 1,
  allow_udp TINYINT(1) NOT NULL DEFAULT 1,
  expires_at DATETIME NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_user_policy (user_id, policy_id),
  INDEX idx_user_id (user_id),
  INDEX idx_policy_id (policy_id),
  INDEX idx_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户可用策略授权';
```

### 配置发布版本表

```sql
CREATE TABLE accel_config_revisions (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  revision VARCHAR(64) NOT NULL UNIQUE COMMENT '例如 20260616.1',
  description VARCHAR(512) NOT NULL DEFAULT '',
  client_config_json JSON NOT NULL COMMENT '下发给客户端的完整配置快照',
  node_policy_json JSON NOT NULL COMMENT '同步给节点的策略快照',
  sha256 CHAR(64) NOT NULL,
  published_by VARCHAR(64) NOT NULL DEFAULT '',
  published_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  INDEX idx_published_at (published_at),
  INDEX idx_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='配置发布版本';
```

## 后台生成的客户端配置

客户端配置面向本机分流，包含游戏进程、TCP/UDP 规则、节点列表、短期 token。

```json
{
  "version": 1,
  "revision": "20260616.1",
  "user_id": "user-1",
  "device_id": "win-device-1",
  "games": [
    {
      "game_id": "steam",
      "name": "Steam",
      "platform": "windows",
      "processes": [
        {
          "process_name": "steam.exe"
        },
        {
          "process_name": "steamwebhelper.exe"
        }
      ],
      "policies": [
        {
          "policy_id": "steam-web-v1",
          "rules": [
            {
              "rule_id": "steam-store-tcp-443",
              "network": "tcp",
              "target_type": "domain",
              "target_value": "store.steampowered.com",
              "port_start": 443,
              "port_end": 443,
              "action": "quic_relay"
            },
            {
              "rule_id": "steam-community-tcp-443",
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
    },
    {
      "game_id": "example_game",
      "name": "Example Game",
      "platform": "windows",
      "processes": [
        {
          "process_name": "game.exe"
        }
      ],
      "policies": [
        {
          "policy_id": "example-game-realtime-v1",
          "rules": [
            {
              "rule_id": "example-game-udp-27000-27999",
              "network": "udp",
              "target_type": "cidr",
              "target_value": "0.0.0.0/0",
              "port_start": 27000,
              "port_end": 27999,
              "action": "quic_relay"
            },
            {
              "rule_id": "example-game-tcp-login-443",
              "network": "tcp",
              "target_type": "domain_suffix",
              "target_value": ".example-game.com",
              "port_start": 443,
              "port_end": 443,
              "action": "quic_relay"
            }
          ]
        }
      ]
    }
  ],
  "nodes": [
    {
      "node_id": "node-test-01",
      "region": "test",
      "endpoint_host": "195.245.242.9",
      "endpoint_port": 5555,
      "alpn": "gaccel/1",
      "sni": "",
      "insecure": true,
      "token": "short-lived-jwt",
      "policies": [
        "steam-web-v1",
        "example-game-realtime-v1"
      ]
    }
  ]
}
```

客户端执行逻辑：

```text
1. 识别发起连接的进程。
2. 根据 process_name 找到 game_id。
3. 根据 network + target + port 命中 rule。
4. action=quic_relay 时选择可用节点。
5. OPEN_TCP / OPEN_UDP 带 metadata。
6. 未命中规则时 direct，禁止默认全局转发。
```

## 后台生成的节点策略配置

节点策略面向安全校验，不包含客户端进程拦截逻辑。

```json
{
  "revision": "20260616.1",
  "node_id": "node-test-01",
  "policies": [
    {
      "policy_id": "steam-web-v1",
      "game_id": "steam",
      "allow_tcp": true,
      "allow_udp": false,
      "rules": [
        {
          "rule_id": "steam-store-tcp-443",
          "network": "tcp",
          "target_type": "domain",
          "target_value": "store.steampowered.com",
          "port_start": 443,
          "port_end": 443,
          "action": "quic_relay"
        },
        {
          "rule_id": "steam-community-sub-tcp-443",
          "network": "tcp",
          "target_type": "domain_suffix",
          "target_value": ".steamcommunity.com",
          "port_start": 443,
          "port_end": 443,
          "action": "quic_relay"
        }
      ]
    },
    {
      "policy_id": "example-game-realtime-v1",
      "game_id": "example_game",
      "allow_tcp": true,
      "allow_udp": true,
      "rules": [
        {
          "rule_id": "example-game-udp-27000-27999",
          "network": "udp",
          "target_type": "cidr",
          "target_value": "0.0.0.0/0",
          "port_start": 27000,
          "port_end": 27999,
          "action": "quic_relay"
        }
      ]
    }
  ]
}
```

节点配置可落到 `config.yaml`：

```yaml
route_policies:
  revision: "20260616.1"
  policies:
    - policy_id: "steam-web-v1"
      game_id: "steam"
      allow_tcp: true
      allow_udp: false
      rules:
        - rule_id: "steam-store-tcp-443"
          network: "tcp"
          target_type: "domain"
          target_value: "store.steampowered.com"
          port_start: 443
          port_end: 443
          action: "quic_relay"
```

## 示例数据

### Steam TCP 商店/社区

```sql
INSERT INTO accel_games (game_id, name, platform)
VALUES ('steam', 'Steam', 'windows');

INSERT INTO accel_game_processes (game_id, process_name)
VALUES
('steam', 'steam.exe'),
('steam', 'steamwebhelper.exe'),
('steam', 'steamservice.exe');

INSERT INTO accel_policies (policy_id, game_id, name, config_revision)
VALUES ('steam-web-v1', 'steam', 'Steam 商店社区 TCP/HTTPS', '20260616.1');

INSERT INTO accel_policy_rules
(rule_id, policy_id, game_id, network, target_type, target_value, port_start, port_end, action, priority)
VALUES
('steam-store-tcp-443', 'steam-web-v1', 'steam', 'tcp', 'domain', 'store.steampowered.com', 443, 443, 'quic_relay', 10),
('steam-community-tcp-443', 'steam-web-v1', 'steam', 'tcp', 'domain', 'steamcommunity.com', 443, 443, 'quic_relay', 10),
('steamcommunity-sub-tcp-443', 'steam-web-v1', 'steam', 'tcp', 'domain_suffix', '.steamcommunity.com', 443, 443, 'quic_relay', 20),
('steamstatic-sub-tcp-443', 'steam-web-v1', 'steam', 'tcp', 'domain_suffix', '.steamstatic.com', 443, 443, 'quic_relay', 20),
('steamcontent-sub-tcp-443', 'steam-web-v1', 'steam', 'tcp', 'domain_suffix', '.steamcontent.com', 443, 443, 'quic_relay', 20);
```

### 通用游戏 UDP/TCP

```sql
INSERT INTO accel_games (game_id, name, platform)
VALUES ('example_game', 'Example Game', 'windows');

INSERT INTO accel_game_processes (game_id, process_name)
VALUES ('example_game', 'game.exe');

INSERT INTO accel_policies (policy_id, game_id, name, config_revision)
VALUES ('example-game-realtime-v1', 'example_game', 'Example Game TCP/UDP 加速', '20260616.1');

INSERT INTO accel_policy_rules
(rule_id, policy_id, game_id, network, target_type, target_value, port_start, port_end, action, priority)
VALUES
('example-game-udp-27000-27999', 'example-game-realtime-v1', 'example_game', 'udp', 'cidr', '0.0.0.0/0', 27000, 27999, 'quic_relay', 10),
('example-game-tcp-login-443', 'example-game-realtime-v1', 'example_game', 'tcp', 'domain_suffix', '.example-game.com', 443, 443, 'quic_relay', 20);
```

### 节点与授权

```sql
INSERT INTO accel_nodes
(node_id, name, region, country, provider, line_type, endpoint_host, endpoint_port, alpn, allow_tcp, allow_udp)
VALUES
('node-test-01', '测试节点01', 'test', 'JP', 'test-provider', 'premium', '195.245.242.9', 5555, 'gaccel/1', 1, 1);

INSERT INTO accel_node_tags (node_id, tag)
VALUES
('node-test-01', 'quic'),
('node-test-01', 'steam'),
('node-test-01', 'udp');

INSERT INTO accel_policy_nodes (policy_id, node_id, priority, weight)
VALUES
('steam-web-v1', 'node-test-01', 10, 100),
('example-game-realtime-v1', 'node-test-01', 10, 100);

INSERT INTO accel_user_policy_grants
(user_id, policy_id, max_connections, rate_limit_mbps, allow_tcp, allow_udp, expires_at)
VALUES
('user-1', 'steam-web-v1', 2, 50, 1, 0, DATE_ADD(NOW(), INTERVAL 30 DAY)),
('user-1', 'example-game-realtime-v1', 2, 50, 1, 1, DATE_ADD(NOW(), INTERVAL 30 DAY));
```

## 后台需要提供的接口

### 客户端拉配置

```http
GET /api/accelerator/client-config?device_id=win-device-1
Authorization: Bearer <user-login-token>
```

返回：

```json
{
  "revision": "20260616.1",
  "expires_in_seconds": 300,
  "config": {
    "games": [],
    "nodes": []
  }
}
```

### 客户端申请节点 token

```http
POST /api/accelerator/token
Authorization: Bearer <user-login-token>
Content-Type: application/json

{
  "device_id": "win-device-1",
  "node_id": "node-test-01",
  "policy_ids": ["steam-web-v1", "example-game-realtime-v1"],
  "ttl_seconds": 3600
}
```

返回：

```json
{
  "token_type": "Bearer",
  "token": "short-lived-jwt",
  "expires_at": "2026-06-16T15:01:52+08:00"
}
```

### 面板同步节点策略

节点策略可以通过完整 `apply_config` 或独立 `apply_policy` 运维命令同步。策略频繁变更时，建议优先使用 `apply_policy`，只替换节点配置里的 `route_policies` 块：

```json
{
  "node_id": "node-test-01",
  "revision": "20260616.1",
  "sha256": "policy-json-sha256",
  "policy": {
    "policies": []
  }
}
```

## 节点已实现能力

当前节点已经支持通用 TCP/UDP 转发和 `policy_id` 级强校验：

1. `protocol.Message.Metadata` 解析为结构化 flow metadata。
2. token claims 支持 `game_ids`、`policy_ids`、`config_revision`。
3. 节点配置支持 `route_policies`。
4. `OPEN_TCP` / `OPEN_UDP` 创建 flow 前执行 token、metadata、policy、rule、目标和端口校验。
5. `/status` 输出 `route_policies.revision` 与 `policy_count`。
6. `/sessions` 输出每个 flow 的 `game_id`、`policy_id`、`rule_id`。
7. flow metrics 按 `game_id`、`policy_id` 聚合。
8. 面板命令支持 `apply_policy` 独立热更新策略块。

## 最小落地顺序

建议业务后台和节点按这个顺序推进：

1. 业务后台先落 MySQL 表。
2. 后台生成客户端配置 JSON。
3. 后台生成节点策略 JSON。
4. token API 按用户授权签发 `game_ids` / `policy_ids` / `config_revision`。
5. 面板用 `apply_policy` 同步节点 `route_policies`。
6. 客户端接入进程级分流。
7. 联调 Steam TCP。
8. 联调一个 UDP 游戏。
9. 再扩展更多游戏规则。
