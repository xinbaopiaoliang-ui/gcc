# 通用 QUIC Relay 服务端开发计划

## 开发约定

- 每次开始开发前，必须先阅读本文件。
- 每完成一项开发任务，必须在本文件对应清单中标注已完成。
- 当前版本只做服务端内核，不做本地客户端产品，不做 VPN 下发，不做 V2bX 面板拉节点。
- 服务端不内置具体游戏库，前期只根据客户端配置产生的连接请求执行转发。
- 服务端必须保留基础安全边界，避免成为开放代理。

## 产品定位

项目定位为通用游戏加速 QUIC Relay 服务端。

客户端负责根据本地配置决定哪些目标需要加速，并向服务端发起连接请求。服务端只负责鉴权、创建 flow、TCP/UDP 转发、限速、统计、健康检查和管理 API。

## 核心架构

```text
客户端配置文件
  -> 客户端
  -> QUIC Control Stream: 鉴权、打开/关闭 flow、心跳
  -> QUIC Datagram: UDP 游戏数据
  -> QUIC Stream: TCP 转发
  -> 服务端内核
  -> 安全策略
  -> TCP/UDP Relay
  -> 目标游戏/业务服务器
```

## 服务端模块

```text
cmd/server            服务端入口
internal/config       配置加载、默认值、热重载
internal/quicserver   QUIC Listener、TLS、ALPN、连接生命周期
internal/protocol     控制消息、错误码、Datagram 头部
internal/auth         JWT/HMAC/开发模式 Token 鉴权
internal/session      QUIC 连接、用户、flow、超时管理
internal/relay        UDP/TCP 转发
internal/router       基础目标安全策略、端口黑白名单
internal/limiter      用户限速、连接数、flow 数限制
internal/traffic      流量统计
internal/metrics      Prometheus 与状态数据
internal/admin        health/status/metrics/config reload
internal/logging      结构化日志、审计日志
```

## 协议草案

### 通道

- Control Stream: 可靠控制信令。
- QUIC Datagram: 低延迟 UDP 数据，不做可靠重传。
- QUIC Stream: TCP 转发。

### 控制消息

- HELLO
- AUTH
- AUTH_OK
- OPEN_UDP
- OPEN_TCP
- CLOSE_FLOW
- PING
- PONG
- ERROR
- STATS

### UDP Datagram 头部

```text
version    1 byte
type       1 byte
flow_id    4 bytes
seq        4 bytes
flags      1 byte
payload    n bytes
```

## 服务端安全底线

- 默认拒绝未鉴权连接。
- 默认拒绝内网、本机、链路本地、多播、云元数据地址。
- 支持 TCP/UDP 端口黑白名单。
- 支持单连接最大 flow 数。
- 支持单用户最大连接数。
- 支持 token 过期时间。
- 管理 API 默认只监听本机地址。

## 配置样例目标

```yaml
server:
  listen: ":443"
  alpn: "gaccel/1"
  cert_file: "./cert.pem"
  key_file: "./key.pem"

auth:
  mode: "dev"
  dev_tokens:
    - "dev-token"

limits:
  max_quic_connections: 50000
  max_user_connections: 8
  max_flows_per_conn: 256
  quic_idle_timeout: "60s"
  udp_idle_timeout: "60s"
  tcp_idle_timeout: "10m"
  user_rate_limit_mbps: 100

security:
  deny_private_ip: true
  deny_loopback: true
  deny_link_local: true
  deny_multicast: true
  deny_cloud_metadata: true
  allowed_udp_ports: ["1-65535"]
  allowed_tcp_ports: ["80", "443", "1935", "5222", "27000-65535"]
  blocked_tcp_ports: ["22", "25", "3306", "5432", "6379"]

admin:
  listen: "127.0.0.1:9090"
```

## 开发清单

### 阶段 0：项目启动

- [x] 写入本开发计划文档。
- [x] 初始化 Go 模块。
- [x] 建立基础目录结构。
- [x] 提供示例配置文件。
- [x] 提供 README。

### 阶段 1：QUIC 服务端骨架

- [x] 配置加载与默认值。
- [x] TLS 证书加载。
- [x] QUIC Listener。
- [x] ALPN 配置。
- [x] 接收连接并创建连接上下文。
- [x] Control Stream 读取循环。
- [x] health 管理接口。

### 阶段 2：鉴权与控制协议

- [x] 定义控制消息结构。
- [x] HELLO。
- [x] AUTH。
- [x] 开发模式静态 token 鉴权。
- [x] AUTH_OK / ERROR。
- [x] PING / PONG。
- [x] 连接级超时与关闭。

### 阶段 3：UDP Datagram Relay

- [x] OPEN_UDP。
- [x] flow_id 分配与查找。
- [x] Datagram 编解码。
- [x] UDP upstream socket 管理。
- [x] UDP 响应回传。
- [x] idle timeout。
- [x] send queue 满时丢弃旧包。

### 阶段 4：TCP Stream Relay

- [x] OPEN_TCP。
- [x] 每个 TCP flow 使用独立 QUIC stream。
- [x] TCP dial timeout。
- [x] 双向转发。
- [x] TCP flow 关闭通知。
- [x] TCP 连接数限制。

### 阶段 5：安全策略与限制

- [x] 私网/本机/链路本地/多播目标拒绝。
- [x] 云元数据地址拒绝。
- [x] TCP/UDP 端口黑白名单。
- [x] 单连接最大 flow 数限制。
- [x] 单用户连接数限制。
- [x] 节点级连接数限制。
- [x] 基础速率限制。

### 阶段 6：统计与管理

- [x] 在线 QUIC 连接数统计。
- [x] 在线 UDP/TCP flow 数统计。
- [x] 用户上下行流量统计。
- [x] flow 创建/关闭原因统计。
- [x] /status。
- [x] /sessions。
- [x] /metrics。
- [x] /config/reload。

### 阶段 7：部署与验证

- [x] systemd service。
- [x] Dockerfile。
- [x] 日志轮转建议。
- [x] 协议文档。
- [x] 测试模拟工具。
- [x] UDP 延迟/吞吐压测。
- [x] pprof。
- [x] 编译与基础测试通过。

### 阶段 8：GitHub 打包发布

- [x] GitHub Actions Release workflow。
- [x] Linux amd64/arm64 交叉编译。
- [x] Release tar.gz 打包。
- [x] SHA256SUMS 校验文件。
- [x] Linux 一键安装脚本。
- [x] README 安装说明。
- [x] 部署文档发布说明。

### 阶段 9：正式鉴权与 Token 工具

- [x] HMAC/JWT 短期 token 验证。
- [x] token 过期时间与 leeway。
- [x] token 用户 ID / 设备 ID claims。
- [x] token 覆盖单用户连接数。
- [x] token 覆盖连接限速。
- [x] token 控制 TCP/UDP 权限。
- [x] gaccel-token 生成工具。
- [x] 鉴权文档与配置示例。
- [x] Release workflow 打包 token 工具。
- [x] /sessions 输出 device_id、有效限额和 TCP/UDP 权限，方便面板侧排查。

### 阶段 10：v0.3.0 客户端联调协议稳定版

- [x] HELLO/AUTH_OK 返回协议版本、ALPN、服务端能力和保活建议。
- [x] AUTH 支持 client_id、client_version、client_platform 元信息。
- [x] /sessions 输出 client_id、client_version、client_platform、last_ping_at、connected_duration。
- [x] 明确鉴权错误码：auth_failed、token_expired、token_not_active、token_missing_exp、token_invalid。
- [x] 明确转发错误码：permission_denied、target_denied、rate_limited、max_flows_exceeded、open_udp_failed、open_tcp_failed。
- [x] gaccel-probe 支持模拟客户端元信息和持续保活测试。
- [x] 客户端联调协议文档补齐握手、保活、重连、token 过期策略。
- [x] 回归测试覆盖 token 错误分类和 session metadata。

### 阶段 11：v0.3.1 Token 获取最小 API

- [x] 独立提供 gaccel-token-api 服务，负责签发短期 HMAC token。
- [x] token API 使用 API Key 鉴权，避免 hmac_secret 暴露给客户端。
- [x] 支持 user_id、device_id、ttl、连接数、限速、TCP/UDP 权限参数。
- [x] 对 ttl、连接数、限速做服务端上限限制。
- [x] 提供 /health 与 POST /token 接口。
- [x] 提供 token-api.example.yaml 与 systemd 示例。
- [x] Release workflow 打包 gaccel-token-api。
- [x] 安装脚本安装 gaccel-token-api 二进制与示例配置。
- [x] 补充 token 获取接口文档与联调 curl 示例。

### 阶段 12：节点运维与面板对接

- [x] 增加节点 ID、区域、标签等节点元数据配置。
- [x] 增加节点到面板的 heartbeat/report 客户端，只上报节点状态和版本，不做客户端订阅下发。
- [x] 设计面板下发运维指令的签名校验，避免管理 API 暴露公网。
- [x] 支持节点配置包校验、热更新和失败回滚。
- [x] 支持节点二进制版本检查和安全升级流程。
- [x] 补充面板对接协议文档。

### 阶段 13：Steam 社区 QUIC 原生联调测试包

- [x] gaccel-probe 支持 HTTPS/Steam 模式，通过 QUIC OPEN_TCP 直连节点转发 TCP。
- [x] Steam 模式默认连接 steamcommunity.com:443 并执行 TLS/HTTPS GET。
- [x] 支持 client_id、client_version、client_platform 元信息。
- [x] 提供 Windows exe 测试包、启动脚本和说明文档。
- [x] 提供 Steam 客户端社区访问联调边界说明：测试包只验证 QUIC 节点转发，不做 SOCKS/TUN/VPN。
- [x] 后续补充 Rust 客户端开发文档。

### 阶段 14：Steam 社区页面 Demo 联调工具

- [x] 提供 gaccel-steam-demo Windows exe，启动本地 Web 控制台。
- [x] 页面支持编辑节点地址、Token API、JWT Token、Steam 目标和客户端元信息。
- [x] 页面支持保存/加载本地 JSON 配置文件。
- [x] 页面支持通过 Token API 申请短期 JWT Token。
- [x] 页面支持执行 QUIC 原生 Steam 社区 HTTPS 测试并显示日志、状态码、延迟和响应预览。
- [x] 提供 demo 测试包和说明文档。

### 阶段 15：v0.3.2 联调发布整理

- [x] Release workflow 打包 Windows Steam Demo。
- [x] Linux Release 包补充 Rust 客户端联调文档。
- [x] README 和部署文档更新到 v0.3.2。
- [x] 本地交叉构建验证 Linux 节点包与 Windows Demo。
- [x] 推送 v0.3.2 tag 触发 GitHub Release。

### 阶段 16：v0.3.3 安全升级联调与结果回传

- [x] 面板命令执行结果进入节点状态快照和 heartbeat/report，便于确认 `stage_upgrade` 是否成功。
- [ ] 提供 v0.3.3 发布包并验证 `stage_upgrade` 暂存流程。
- [ ] 编写面板下发 `stage_upgrade` 的端到端测试脚本或示例。
- [ ] 补充升级切换与人工回滚 Runbook。

### 阶段 17：v0.4.0 客户端真实流量联调 Demo

- [x] 提供 HTTP CONNECT 到 QUIC `OPEN_TCP` 的 Windows 联调 demo，用于浏览器/Steam WebView 访问 Steam 商店和论坛。
- [x] demo 支持 `-steam-client-mode`，临时设置 Windows 当前用户系统代理并拉起 Steam 客户端进行商店/社区联调。
- [x] demo 默认只监听本机地址，只允许 Steam 相关域名和 443 端口，避免成为开放代理。
- [x] Release workflow 打包 `gaccel-connect-demo_<version>_windows-amd64.zip`。
- [x] 补充 CONNECT demo 使用文档和 Rust 客户端参考说明。

### 阶段 18：业务后台游戏配置与节点策略模型

- [x] 输出业务后台 MySQL 表结构、客户端配置格式、节点策略格式和 TCP/UDP flow metadata 文档。

### 阶段 19：客户端与节点正式联调协议文档

- [x] 输出面向客户端/AI 开发的严谨联调协议说明，明确连接状态机、TCP/UDP flow、metadata、错误处理和禁止行为。

### 阶段 20：v0.4.1 节点策略配置读取与 matcher

- [x] 配置增加 `route_policies.revision`、`policies`、`rules`，支持 TCP/UDP、域名、域名后缀、IP、CIDR、端口范围和 action 校验。
- [x] 实现 `FlowMetadata` 结构化解析，校验 `game_id`、`policy_id`、`rule_id`、`network`、`client_config_revision`。
- [x] 实现 route policy matcher，并覆盖放行、拒绝、CIDR UDP、无策略兼容等单元测试。

### 阶段 21：v0.4.2 token game_ids / policy_ids 授权

- [x] HMAC token 增加 `game_ids`、`policy_ids`、`config_revision` claims，并保留旧 `games` 字段兼容。
- [x] `gaccel-token` 增加 `-game-ids`、`-policy-ids`、`-config-revision` 参数。
- [x] Token API 支持业务后台签发带策略授权的短期 token。

### 阶段 22：v0.4.3 OPEN_TCP / OPEN_UDP 策略强校验

- [x] `OPEN_TCP` 和 `OPEN_UDP` 在创建 flow 前执行 token 授权、metadata、policy、rule、目标和端口校验。
- [x] 启用 `route_policies` 后，缺失 metadata 或策略不匹配会返回 `target_denied`。
- [x] 无 `route_policies` 的旧配置保持兼容，继续只走基础安全策略。

### 阶段 23：v0.4.4 状态、日志、sessions、metrics 完善

- [x] `/status` 输出 route policy revision 和 policy_count。
- [x] `/sessions` 输出 token 授权的 game_ids、policy_ids、config_revision，以及 flow 的 game_id、policy_id、rule_id。
- [x] flow metrics 事件增加 game_id、policy_id 标签，并保留旧指标兼容。

### 阶段 24：v0.4.5 面板 apply_policy 独立策略热更新

- [x] Manager 支持只替换 `route_policies` 配置块，并保留 SHA256 校验、热更新和失败回滚。
- [x] 面板命令新增 `apply_policy`，支持独立下发策略 YAML。
- [x] `gaccel-probe` 和 Steam CONNECT demo 支持策略 metadata，便于强策略节点联调。
- [x] 已部署到 `195.245.242.9` 并验证：节点版本 `0.4.5-local`，Steam 商店公网 QUIC 转发成功，无 metadata 请求被拒绝。

### 阶段 25：v0.4.6 策略联调硬化

- [x] 面板 heartbeat/report payload 增加 `route_policies.revision` 和 `policy_count`，便于后台确认节点当前策略版本。
- [x] 为 `apply_policy` 增加单元测试，覆盖 SHA256、策略块替换、节点配置保留和执行结果 details。
- [x] 同步面板、部署、客户端和业务后台文档，明确 v0.4 策略强校验已实现。
- [x] 已部署到 `195.245.242.9` 并验证：节点版本 `0.4.6-local`，服务 active，公网 QUIC 策略转发 Steam 商店成功。

### 阶段 26：v0.5.0 临时控制面板规划

- [x] 明确控制面板与业务后台、服务器节点的职责边界。
- [x] 明确技术选型：Go、MySQL 8.0.45、React、TypeScript、Vite、Ant Design。
- [x] 输出控制面板数据库表、API、任务系统、SSH 部署、一键更新和策略同步计划。
- [x] 新增 `cmd/gaccel-panel` 后端骨架。
- [x] 新增控制面板 MySQL schema 文件。
- [x] 新增控制面板基础配置加载。
- [x] 新增 `/health` 接口。

### 阶段 27：v0.5.1 控制面板节点 CRUD 与业务后台同步

- [x] 明确控制面板开发/测试环境：`103.201.131.99`，宝塔 + MySQL 8.0.45。
- [x] 新增控制面板 MySQL store，连接 `panel_nodes` 与 `panel_audit_logs`。
- [x] 新增 `/api/panel/nodes` 节点 CRUD API。
- [x] 新增 `/api/backend/nodes` 业务后台节点同步 API。
- [x] 节点 API 使用 Bearer API key 临时鉴权，避免未登录阶段裸露。
- [x] 节点增删改写入操作审计。
- [x] 节点列表和详情页：新增 `web/panel` React + Ant Design 页面，支持节点列表、筛选、创建/编辑弹窗、详情抽屉和删除操作，并由 `gaccel-panel` 托管静态构建产物。
- [x] 在 `103.201.131.99` 上导入 schema 并部署 `gaccel-panel`：MySQL schema 已导入 `gaccel_panel`，二进制和配置位于 `/www/server/gaccel-panel`，宝塔 Supervisor 进程 `gaccel_panel` 运行中，本机 `/health` 与 `/api/panel/nodes` 已验证通过。
- [x] 公网入口确认：`http://103.201.131.99:8090/health` 已通，未授权访问 `/api/panel/nodes` 返回 401，带 Bearer API key 访问 `/api/panel/nodes` 返回空节点列表。
- [x] 公网页面部署验证：`http://103.201.131.99:8090/` 已返回控制面板页面，静态资源加载正常；后续 v0.5.6 已将 API Key 输入入口替换为登录页。

### 阶段 28：v0.5.2 控制面板节点 report 与 command

- [x] 控制面板接收 `POST /api/nodes/report`，保存节点 heartbeat/report 快照。
- [x] 上报后更新 `panel_nodes` 最新状态、版本、策略版本、最近上报时间和错误信息。
- [x] `panel_commands` 执行结果回写 `panel_node_tasks`，支持 success/failed/cancelled 状态对账。
- [x] 控制面板实现 `GET /api/nodes/commands?node_id=<node_id>`，支持节点主动拉取 pending 命令，并返回 HMAC 签名 envelope。
- [x] 控制面板实现 `apply_policy` 任务创建接口，支持策略 YAML、可选 revision、可选 SHA256 自动计算。
- [x] React 控制面板详情页展示最近上报和最近任务，节点列表与详情页支持创建 `apply_policy` 任务。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。

### 阶段 29：v0.5.3 SSH 凭据与一键部署

- [x] 新增 SSH 凭据模型和接口：`GET/PUT/DELETE /api/panel/nodes/{node_id}/credential`。
- [x] SSH 密码、私钥和私钥 passphrase 使用 `security.master_key` 派生 AES-GCM 密钥加密保存，API 不回显明文。
- [x] 新增 `POST /api/panel/nodes/{node_id}/credential/test`，只做 SSH 连通性轻量探测，不改服务器文件。
- [x] React 控制面板详情页展示凭据状态，并支持保存、删除、测试 SSH 凭据。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`gaccel-panel` 版本 `0.5.3-local`，公网 `/health` 返回 ok，授权节点列表 200，凭据接口 200，未授权访问 401。
- [x] 新增 `POST /api/panel/nodes/{node_id}/deploy` 一键部署任务，后台通过 SSH 执行安装、配置写入、证书生成、systemd 启动。
- [x] 新增 `GET /api/panel/tasks/{task_id}/logs` 部署日志查询，任务日志写入 `panel_node_task_logs`。
- [x] 部署完成后自动检查节点本机 `/health`、`/status` 和 `/usr/local/bin/gaccel-node -version`，并回写任务结果和节点状态。
- [x] React 控制面板支持创建部署任务和查看任务日志。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.3-local`，前端页面 200，授权节点列表 200，任务日志接口 200，部署接口参数校验 400，未授权访问 401。

### 阶段 30：v0.5.4 控制面板一键更新与回滚

- [x] 新增 `POST /api/panel/nodes/{node_id}/update` 一键更新任务，后台通过 SSH 创建 `update_node` 任务并写入审计。
- [x] 更新前备份 `/usr/local/bin/gaccel-*`、节点 systemd service 和 `/etc/gaccel-node/config.yaml` 到 `/var/lib/gaccel-node/backups/<task_id>`。
- [x] 更新任务通过目标服务器拉取 GitHub release install script 安装指定版本，重启 `gaccel-node` 后检查本机 `/health`、`/status` 与 `/usr/local/bin/gaccel-node -version`。
- [x] health/status/version 任一步失败时自动恢复备份二进制并重启 `gaccel-node`，任务结果记录 `rolled_back` 和 `backup_dir`。
- [x] React 控制面板详情页新增“更新”按钮和更新弹窗，复用现有任务日志抽屉查看执行过程。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.4-local`，前端页面 200，授权节点列表 200，任务日志接口 200，更新接口参数校验 400，未授权访问 401，Supervisor 进程 running。

### 阶段 31：v0.5.5 控制面板策略同步闭环

- [x] 新增策略版本模型与 API：`GET/POST /api/panel/policy-revisions`、`GET/POST /api/backend/policy-revisions`，保存 `revision`、`sha256`、`route_policies_yaml` 和来源。
- [x] 新增节点期望策略接口：`POST /api/panel/nodes/{node_id}/desired-policy` 与 `POST /api/backend/nodes/{node_id}/desired-policy`。
- [x] 提交 desired policy 后写入 `panel_nodes.desired_policy_revision` 与 `panel_node_policy_revisions`，并可自动创建 `apply_policy` 任务。
- [x] 节点上报 `route_policies.revision` 后自动把匹配的 `panel_node_policy_revisions` 标记为 `applied`，失败命令写回 `last_error`。
- [x] React 控制面板节点详情页支持选择已保存策略版本并同步到节点，展示 SHA256 和 YAML 预览。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.5-local`，公网首页 200，未授权节点列表 401，授权策略版本保存/列表 200，缺失节点 desired policy 返回 `node_not_found`，Supervisor 进程 running。

### 阶段 32：v0.5.6 控制面板登录与 Session 鉴权

- [x] 新增面板账号登录接口：`POST /api/panel/login`、`POST /api/panel/logout`、`GET /api/panel/me`。
- [x] 面板人工操作 API 改为使用 `gaccel_panel_session` HttpOnly Cookie；业务后台和节点接口继续使用 Bearer API Key。
- [x] 新增 `panel_users` 读取与 bcrypt 密码校验，禁用用户无法登录。
- [x] 新增 `gaccel-panel -create-admin` 管理员创建/重置命令，部署时无需手写 MySQL 密码哈希。
- [x] React 控制面板新增登录页、登录态检查和退出登录，移除公开的 Backend API Key 输入框。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.6-local`，`admin` 登录成功，未登录 `/api/panel/nodes` 返回 401，登录后 `/api/panel/nodes` 返回 200，公网首页 200。

### 阶段 33：v0.5.7 面板登录安全加固

- [x] 新增登录失败限流：按 `IP + username` 统计失败登录，短时间连续失败后返回 `login_rate_limited`。
- [x] 新增当前账号改密接口：`POST /api/panel/me/password`，需要登录态和当前密码，密码使用 bcrypt 重新写入 `panel_users`。
- [x] 改密成功后刷新 `gaccel_panel_session` Cookie，并记录 `panel.password.change` 审计日志。
- [x] React 控制面板右上角新增“改密”入口和修改密码弹窗，支持当前密码、新密码校验和错误提示。
- [x] 本地验证通过：`go test ./...` 与 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.7-local`，失败登录第 6 次返回 429，改密后旧密码登录 401，恢复后 admin 登录 200。

### 阶段 34：v0.5.8 控制面板账号管理与角色权限

- [x] 后端新增 `GET/POST /api/panel/users`、`PUT /api/panel/users/{id}`、`POST /api/panel/users/{id}/password`，支持管理员创建账号、调整角色/状态、重置其他账号密码。
- [x] 新增 `admin`、`operator`、`viewer` 角色边界：管理员可执行节点写操作、凭据、部署、更新、策略下发和账号管理；普通角色只允许查看节点、上报、任务和日志。
- [x] 增加自保护：当前管理员不能在账号管理里降级、禁用自己，也不能通过账号管理重置自己的密码，必须走 `/api/panel/me/password`。
- [x] React 控制面板新增账号与角色页面，支持账号列表、新建账号、编辑角色/状态、重置密码。
- [x] React 节点列表和详情页按角色隐藏高风险操作入口，普通角色保留只读查看能力。
- [x] 本地验证通过：`go test ./...` 和 `web/panel npm run build`。
- [x] 已部署到 `103.201.131.99`：`/health` 版本 `0.5.8-local`；管理员账号管理接口 200；临时 operator 可登录并查看节点列表；operator 访问账号管理和创建节点均返回 403；禁用后登录返回 401。

### 阶段 35：v0.5.9 前后端分离与 JWT 面板登录

- [x] 面板人工操作 API 改为 `Authorization: Bearer <panel_access_token>`，解决前端静态站和 Go 后端分端口部署时的跨站 Cookie 问题。
- [x] `POST /api/panel/login` 返回 `access_token`、`token_type`、过期时间和当前用户信息。
- [x] `POST /api/panel/me/password` 改密成功后返回新的 Bearer JWT，前端自动刷新本地 token。
- [x] 前端通过 `panel-config.js` 运行时配置 `apiBaseURL`，默认调用 `http://103.201.131.99:8091`。
- [x] 后端新增 `cors.allowed_origins` 和 `GACCEL_PANEL_CORS_ALLOWED_ORIGINS`，允许 `http://103.201.131.99:9788` 调用 Go API。
- [x] 登录页文案改为 Bearer JWT，节点创建弹窗创建态不再把示例默认值填进输入框。
- [x] 前端静态包和 Go 后端包分开打包，分别适配宝塔 PHP 项目和 Go 项目。
- [x] 本地验证通过：`go test ./internal/panel ./cmd/gaccel-panel` 与 `web/panel npm run build`。

### 阶段 36：v0.5.10 账号权限页面优化

- [x] 账号权限页增加本地搜索、角色筛选、状态筛选和当前显示数量。
- [x] 账号权限页增加账号总数、启用账号、管理员、禁用账号摘要。
- [x] 账号表格优化为身份头像、当前账号高亮、禁用账号弱化和文字操作按钮。
- [x] 空状态、移动端布局和账号页视觉层级优化。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 37：v0.5.11 节点管理页优化

- [x] 节点总览增加待更新版本、待同步策略等运营摘要。
- [x] 节点表格补充当前/目标版本、当前/目标策略、最近上报和 Admin 地址。
- [x] 节点表格突出版本漂移、策略漂移和最后错误，方便联调排查。
- [x] 节点详情抽屉增加状态、版本、策略、实时连接摘要。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 38：v0.5.12 策略与游戏配置页面

- [x] 新增策略与游戏配置工作台，支持查看策略版本列表。
- [x] 支持保存 `route_policies` YAML 策略版本，并展示 revision、sha256、game_id、policy_id、规则数量和 TCP/UDP 覆盖。
- [x] 支持从策略页选择节点并下发 `desired_policy_revision`，可创建 `apply_policy` 任务。
- [x] 支持策略 YAML 预览、来源筛选、本地搜索和空状态。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 39：v0.5.13 策略预览终端化

- [x] 策略预览弹窗改为黑色终端背景，接近 Xshell 配置查看体验。
- [x] 策略预览增加 revision、game_id、policy_id、规则数量、协议和 SHA256 摘要。
- [x] 自动把转义的 `\n` 转为真实换行，并以行号方式展示格式化后的 YAML。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 40：v0.5.14 策略页面中文标题修正

- [x] 策略预览弹窗中的 Revision、Game ID、Policy ID、Rules、Protocol 等标题改为中文。
- [x] 策略页来源、搜索占位、规则数量、路由策略标题等可见英文统一中文化。
- [x] 同步策略弹窗的标题、按钮、空状态、表单标题和说明改为中文。
- [x] 保留 YAML 内部字段名和 `apply_policy` 等协议命令名，避免影响 route_policies 配置语义。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 41：v0.5.15 策略预览护眼暗色优化

- [x] 策略预览弹窗降低纯黑与高亮文字对比，改为柔和墨蓝灰背景。
- [x] 代码区文字、行号、边框、标签和状态圆点降低饱和度，避免长时间查看刺眼。
- [x] 策略预览弹窗增加独立遮罩样式，压暗后方页面并保留轻微模糊。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 42：v0.5.16 策略校验与差异预览

- [x] 面板后端新增 `POST /api/panel/policy-revisions/validate` 和 `POST /api/backend/policy-revisions/validate`，保存前按节点同语义解析 `route_policies`。
- [x] 策略校验返回 `valid`、`sha256`、错误、警告、策略/规则/游戏统计和基线差异摘要。
- [x] 保存策略版本前强制执行 YAML 语义校验，避免无效策略进入节点同步队列。
- [x] 策略页保存弹窗新增手动校验按钮、校验结果卡片、规则统计和差异摘要。

### 阶段 43：v0.5.17 节点策略同步状态

- [x] 新增 `GET /api/panel/nodes/{node_id}/sync-status` 和 `GET /api/backend/nodes/{node_id}/sync-status`。
- [x] 同步状态返回版本状态、策略状态、上报年龄、任务队列计数、最后错误和排查建议。
- [x] 节点详情页新增同步状态面板，展示当前/目标版本、当前/目标策略、任务队列和上报年龄。

### 阶段 44：v0.5.18 业务后台接口文档与补齐

- [x] 新增 `docs/panel-backend-api.md`，整理业务后台鉴权、节点写入、策略校验、策略保存、desired policy 和 sync-status 接口。
- [x] 明确 `backend_api_key`、节点 `auth.hmac_secret`、`node_command.secret` 和 `security.master_key` 的职责边界。
- [x] 文档补充业务后台下发策略到节点后的状态闭环和字段说明。

### 阶段 45：v0.5.19 任务重试与日志补强

- [x] 后端新增 `POST /api/panel/tasks/{task_id}/retry`，复制终态任务生成新的 pending 任务，保留旧任务和旧日志。
- [x] 部署/更新任务重试后自动重新进入本地 SSH 执行流程，`apply_policy` 重试后等待节点拉取命令。
- [x] 节点详情任务列表和任务日志抽屉增加重试入口。
- [x] 重试任务写入审计日志和新任务日志，便于追踪来源任务。

### 阶段 46：v0.5.20 安全巡检与权限硬化

- [x] 新增 `GET /api/panel/security/overview`，只返回非敏感安全状态，不回显任何 secret 明文。
- [x] 账号与权限页新增安全巡检区，展示 API Key 数量、命令签名、会话 TTL、CORS、SSH 凭据缺口和策略/版本漂移。
- [x] 修复节点命令响应 nonce 回放测试失败问题，nonce 缓存过期判断改为使用同一个校验时间源。
- [x] 本地验证通过：`go test ./...` 和 `web/panel npm run build`。

### 阶段 47：v0.5.21 用户列表空数组兼容热修复

- [x] 后端安全巡检与同步建议中的数组字段改为空数组输出，避免 Go nil slice 编码成 JSON null。
- [x] 前端账号与权限页对用户列表、安全巡检 warnings 和 CORS 来源做运行时兜底，避免切换用户列表时读取 null.length 崩溃。
- [x] 本地验证通过：`go test ./...` 和 `web/panel npm run build`。

### 阶段 48：v0.5.22 面板系统自检

- [x] 新增 `GET /api/panel/system/check`，返回面板配置、数据库、CORS、安全密钥、业务后台 API Key 和部署目录的只读自检结果。
- [x] React 面板新增“系统自检”页面，展示通过、警告、错误数量和具体修复建议。
- [x] 自检结果不回显任何 secret 明文，只返回是否已配置、数量和脱敏部署信息。
- [x] 本地验证通过：`go test ./internal/panel` 和 `web/panel npm run build`。

### 阶段 49：v0.5.23 节点接入诊断

- [x] 新增 `GET /api/panel/nodes/{node_id}/diagnostics`，检查节点 admin `/health`、`/status`、`/panel/commands`、最近上报和策略/版本同步状态。
- [x] 节点列表增加“接入自检”入口，便于确认业务后台、控制面板和节点三方是否连通。
- [x] 诊断接口只读，不下发命令、不修改节点文件、不暴露 SSH 凭据。
- [x] 本地验证通过：`go test ./internal/panel` 和 `web/panel npm run build`。

### 阶段 50：v0.5.24 业务后台 API 稳定版

- [x] 补齐业务后台节点、策略、desired policy、sync-status、system check 的请求/响应字段文档。
- [x] 明确业务后台通过 `node_id` 作为节点主键，IP/端口仅作为节点连接入口和部署信息。
- [x] 输出错误码、重试建议、字段边界和安全密钥职责说明，供业务后台 AI 开发直接对照。

### 阶段 51：v0.5.25 游戏策略可视化编辑

- [x] 策略页新增可视化生成器，支持填写 game_id、policy_id、TCP/UDP、目标域名/IP/CIDR、端口范围和规则动作。
- [x] 生成器输出标准 `route_policies` YAML，并复用后端策略校验和 SHA256 计算。
- [x] 保留手写 YAML 能力，方便业务后台复杂规则导入和人工排障。
- [x] 本地验证通过：`web/panel npm run build`。

### 阶段 52：v0.6.0 客户端/业务后台联调稳定版

- [x] 汇总客户端、业务后台、控制面板和节点的联调文档，明确 token、metadata、route_policies 和节点状态闭环。
- [x] 版本号整理到 v0.6.0，前端静态包和 Go 后端包继续分开打包。
- [x] 输出 v0.6.0 本地验证结果和部署注意事项。

### 阶段 53：v0.6.1 面板远程修复节点 Admin 接入

- [x] 一键部署生成节点配置时，根据面板 Admin Host 自动选择 `admin.listen`：公网/非回环 Admin Host 使用 `0.0.0.0:<admin_port>`，回环地址仍使用 `127.0.0.1:<admin_port>`。
- [x] 新增面板接口 `POST /api/panel/nodes/{node_id}/repair-admin`，通过已保存 SSH 凭据远程备份并修复 `/etc/gaccel-node/config.yaml` 的 Admin 监听。
- [x] 修复任务重启 `gaccel-node` 后校验节点本机 `/health`、`/status`，并从面板侧再次访问 Admin URL。
- [x] 接入自检弹窗新增“修复 Admin 接入”操作，管理员可直接从面板创建修复任务并查看任务日志。

### 阶段 54：v0.6.2 Admin 修复任务可观测性与按钮视觉热修

- [x] “修复 Admin 接入”按钮改为更柔和的墨蓝灰主按钮，降低亮蓝色在诊断弹窗里的突兀感。
- [x] Admin 修复任务本机 `/health` 和 `/status` 校验增加等待重试，避免节点刚重启时误判失败。
- [x] 本机 Admin 校验失败时，任务日志自动采集 `systemctl status gaccel-node`、最近 journal 和 TCP 监听端口，方便直接在面板定位原因。
- [x] 面板侧 Admin 外部探测增加短重试，降低网络瞬时抖动导致的误报。

### 阶段 55：v0.6.3 节点安装与更新失败诊断增强

- [x] 一键部署和一键更新在执行安装脚本前，先打印 GitHub release base URL、`SHA256SUMS` 下载地址和匹配到的架构包名。
- [x] 安装前增加 release archive 探测，目标节点版本不存在或 Release 资源缺失时，任务日志能直接显示失败 URL，而不是只显示 `Process exited with status 22`。
- [x] 更新失败回滚时输出备份目录、已恢复文件、缺失备份文件、`systemctl status gaccel-node` 和最近 journal，便于在面板内定位 `rollback failed` 的真实原因。
- [x] 包内说明明确“节点版本”必须填写真实 `gaccel-node` GitHub Release 版本，不能填写控制面板包版本。

### 阶段 56：v0.6.4 业务后台签发节点 HMAC Secret

- [x] `panel_nodes` 增加 `hmac_secret_encrypted`、`hmac_secret_source`、`hmac_secret_updated_at` 字段，并提供可重复导入的 MySQL 迁移。
- [x] `POST/PUT /api/backend/nodes` 支持业务后台传入 `hmac_secret`，控制面板使用 `security.master_key` 加密保存，不在响应和审计日志中回显明文。
- [x] 节点更新请求未传 `hmac_secret` 时保留旧密钥，支持业务后台后续按需轮换密钥。
- [x] 一键部署任务不再要求人工输入 `hmac_secret`，部署时自动读取节点已保存的加密副本并写入节点配置。

### 阶段 57：v0.6.5 业务后台同步闭环增强

- [x] `sync-status` 返回 `hmac_secret_configured`、`hmac_secret_source`、`hmac_secret_updated_at` 和 `deploy_ready`，业务后台可判断节点是否具备部署和 token 签发条件。
- [x] 系统安全概览统计未同步 HMAC Secret 的节点数，并在 warnings 中提示业务后台补齐密钥。
- [x] 节点接入诊断增加“节点 HMAC Secret”检查项，缺失时给出明确处理建议。

### 阶段 58：v0.6.6 客户端会话观测增强

- [x] 节点接入诊断额外探测 Admin `/sessions`，汇总在线 session、flow、user、device、game_id 和 policy_id。
- [x] 前端部署弹窗移除 HMAC Secret 输入框，改为展示业务后台密钥同步状态，避免运维误把密钥当部署临时参数。
- [x] 更新业务后台接口文档、Token API 文档和面板打包说明，明确 `hmac_secret` 由业务后台生成并保存，客户端只拿短期 JWT。

### 阶段 59：v0.6.7 QUIC UDP Buffer 面板优化任务

- [x] 新增 `POST /api/panel/nodes/{node_id}/tune-udp-buffer`，通过已保存 SSH 凭据远程写入 `/etc/sysctl.d/99-gaccel-quic.conf`。
- [x] 默认阈值设置为 `net.core.rmem_max=16777216` 与 `net.core.wmem_max=16777216`，高于 quic-go 期望值并保持内存占用可控。
- [x] 优化任务执行 `sysctl -p`、重启 `gaccel-node`、验证本机 Admin `/health` 和 `/status`，并检查近期 journal 中不再出现 UDP buffer 警告。
- [x] 节点接入自检弹窗新增“优化 UDP Buffer”按钮，管理员可从控制面板直接创建任务并在任务日志中追踪结果。

## 已知风险与注意事项

- QUIC Datagram 不保证可靠到达，适合 UDP 实时游戏包；控制消息和 TCP 必须走 Stream。
- Datagram payload 需要控制大小，避免超过路径 MTU；第一版建议客户端侧控制在 1200 字节以内。
- 服务端按客户端请求转发，但必须保留基础目标限制，否则会变成开放代理。
- 离线 token 难实时撤销，生产环境应使用短有效期 token。
- 管理 API 不应暴露到公网，除非启用强鉴权或 mTLS。
- 队列策略优先低延迟，实时 UDP 包宁愿丢弃旧包，也不要排队堆积。

### 阶段 60：v0.6.8 Token 会话额度默认值控制台

- [x] 新增 `panel_token_defaults` MySQL 表，保存业务后台签发短期 JWT 时使用的默认档位。
- [x] 默认按游戏加速器标准配置：免费/测试 32、普通 64、高级 128、旗舰 256，节点硬上限 512。
- [x] 新增 `GET/PUT /api/panel/token-defaults`，管理员可在控制台调整默认 `max_connections`、`rate_limit_mbps` 和 TCP/UDP 权限。
- [x] 新增 `GET /api/backend/token-defaults`，业务后台可读取当前默认档位，用于签发客户端短期 JWT。
- [x] 前端系统页新增“客户端会话默认值”设置区，保存时强校验默认连接数不得超过 512。

### 阶段 61：v0.6.9 业务后台对接 API 文档

- [x] 新增 `docs/business-backend-api.md`，按业务后台接入顺序整理 system check、token defaults、节点同步、策略校验、策略保存、desired policy 和 sync-status 接口。
- [x] 文档补充业务后台自有客户端 token 签发接口建议，明确 JWT claims、`hmac_secret` 使用边界和客户端返回字段。
- [x] 请求示例使用带注释的 JSONC，并明确实际请求需要去掉注释，方便业务后台开发直接照着落地。
- [x] 新增 `docs/business-backend-api.apifox.openapi.json`，提供 Apifox 可直接导入的 OpenAPI 3.0 接口文档，覆盖业务后台调用控制面板的 `/api/backend/*` 接口和客户端 token 建议接口。
- [x] 新增 `docs/业务后台游戏配置字段补充说明.md`，根据业务后台提供的数据库字段文档补充运行时转换规则、缺失字段、token、metadata、route_policies 和节点同步边界。

### 阶段 62：v0.6.10 节点流量统计聚合

- [x] 新增 `GET /api/panel/traffic/overview`，按最近 1/6/24/72/168 小时聚合节点上报的流量、连接、用户和 flow 事件。
- [x] 统计数据复用已有 `panel_node_reports.metrics_json`，不新增数据库迁移，避免升级时影响节点正常上报。
- [x] 后端按节点 report 样本计算窗口增量；窗口内只有单样本时退化为节点启动以来累计值，并在响应中返回 `sample_mode`。
- [x] 单元测试覆盖节点总流量、用户排行、flow 打开失败和策略漂移聚合。

### 阶段 63：v0.6.11 控制面板流量统计页面

- [x] 前端新增“流量与联调观测”页面，展示窗口流量、活跃 QUIC、TCP/UDP flow、打开失败和策略漂移。
- [x] 页面支持窗口切换和手动刷新，按节点流量排行、用户流量排行、flow 事件排行展示数据。
- [x] UI 保持运维控制台风格，使用紧凑表格、柔和边框和低饱和状态色，避免营销式卡片堆叠。

### 阶段 64：v0.6.12 客户端联调观测增强

- [x] 流量页面汇总用户维度 `user_id`、活跃连接数、TCP/UDP 上下行字节，便于确认客户端是否真实产生转发。
- [x] flow 事件排行展示 `network`、`event`、`reason`、`game_id` 和 `policy_id`，便于定位 Steam、论坛或其他游戏策略命中情况。
- [x] 面板排障建议根据上报缺失、策略漂移、flow 打开失败和无活跃连接自动生成。

### 阶段 65：v0.6.13 策略版本一致性观测

- [x] 流量统计响应新增 `policy_consistency`，按节点返回当前策略、目标策略、同步状态、上报年龄和最后错误。
- [x] 前端新增“策略一致性”表格，明确区分 `synced`、`pending`、`waiting_report`、`not_set`。
- [x] 总览统计 `policy_drift_nodes`，帮助判断慢或断连是否可能来自策略未下发。

### 阶段 66：v0.6.14 慢速与断连排障视图

- [x] 节点排行展示最近上报时间、上报年龄、QUIC/TCP/UDP 活跃数和 TCP/UDP 分项流量。
- [x] flow 事件排行按失败原因和关闭原因排序，可快速看到 `denied`、`session_closed`、`eof` 等异常集中点。
- [x] 新增“游戏/策略事件”排行，按 `game_id`、`policy_id` 和协议汇总打开、关闭、错误次数。

### 阶段 67：v0.6.15 文档与交付说明

- [x] 新增 `docs/panel-traffic-observability.md` 和 `docs/panel-traffic-observability.apifox.openapi.json`，说明控制面板流量统计接口、字段含义、排障使用方式和 Apifox 导入方式。
- [x] 更新 `docs/panel-backend-api.md`，补充面板 JWT 只读流量统计接口说明，明确该接口不是业务后台 API Key 接口。
- [x] 本阶段无需新增 SQL；如线上面板已有 `panel_node_reports` 表，升级后即可读取历史 report 生成统计。

### 阶段 68：v0.6.16 客户端会话生命周期模型

- [x] 新增 `panel_client_sessions` 和 `panel_client_session_events`，保存客户端连接、认证、断开和断开原因。
- [x] 新增 `migrations/20260623_v0616_client_sessions.sql`，可直接在 MySQL 8 执行升级。
- [x] 控制面板 Store 增加 `ListClientSessions`，支持按节点、用户、设备、状态、断开原因和时间窗口查询。

### 阶段 69：v0.6.17 节点会话事件上报

- [x] 节点 session registry 增加 `session_started`、`session_authenticated`、`session_ended` 事件队列。
- [x] 节点 report payload 增加 `sessions` 与 `session_events`，上报当前在线会话和待确认事件。
- [x] report 成功后确认事件序号，避免失败时丢失断开记录。

### 阶段 70：v0.6.18 心跳超时与断开原因

- [x] 节点配置新增 `limits.heartbeat_interval` 与 `limits.session_disconnect_timeout`，默认 `15s` 与 `45s`。
- [x] QUIC 会话监控最近活跃时间，客户端后台被强关或网络失联时记录 `heartbeat_timeout`。
- [x] 断开原因区分 `client_shutdown`、`heartbeat_timeout`、`quic_idle_timeout`、`network_lost`、`node_shutdown`。

### 阶段 71：v0.6.19 控制面板客户端会话页

- [x] 前端新增“客户端会话”页面，展示在线会话、已结束会话、超时断开和会话流量。
- [x] 页面支持按时间窗口、节点、用户 ID、设备 ID、状态和断开原因筛选。
- [x] 表格展示连接时间、认证时间、结束时间、最后心跳、游戏/策略、TCP/UDP flow 和流量。

### 阶段 72：v0.6.20 会话接口文档与交付

- [x] 新增 `docs/client-session-lifecycle.md`，说明客户端心跳、节点断开判定、控制面板 API 和数据库表。
- [x] 新增 `docs/client-session-lifecycle.apifox.openapi.json`，可导入 Apifox 给业务后台和客户端对接。
- [x] 本阶段本地验证覆盖 Go 局部包测试、前端构建；最终打包时继续输出 Go 后端包和前端静态包。

### 阶段 73：v0.6.21 控制面板布局偏移修复

- [x] 修复固定左侧栏和主内容 `margin-left` 叠加导致页面总宽度超过视口的问题。
- [x] 桌面端将 `.panel-content` 宽度限制为 `calc(100% - 64px)`，移动端恢复 `100%`，避免整页向右偏移。
- [x] 保留表格内部横向滚动，不影响节点列表、流量统计和客户端会话等宽表格页面。

### 阶段 74：v0.6.22 宽屏主区域对齐修复

- [x] 修复 `topbar`、`metric-strip`、`workbench` 在宽屏下仍按 1440px 居中，导致左侧栏和内容之间空白过大的问题。
- [x] 主页面容器改为占满 `.panel-content` 可用宽度，保持左侧对齐并继续由页面内表格处理横向滚动。
- [x] 保留 v0.6.21 的页面级防溢出修复，避免重新出现整页横向滚动。

### 阶段 75：v0.6.23 Grid 页面内部撑宽修复

- [x] 线上复测确认“客户端会话”页内部 grid 隐式列被内容撑宽，`session-header` 宽度超过外层容器。
- [x] `session-panel` 和 `traffic-panel` 显式使用 `grid-template-columns: minmax(0, 1fr)`，避免卡片、筛选区和表格撑开页面。
- [x] 两个 grid 页面直接子项增加 `min-width: 0`，保留表格内部滚动并防止整页视觉右偏。
### 阶段 76：v0.7.1 节点丢包风险体检与 UDP 参数补强

- [x] 新增 `GET /api/panel/nodes/{node_id}/network-diagnostics`，通过已保存 SSH 凭据只读采集节点本机 UDP buffer、UDP socket 队列、网卡 dropped/error、最近 `gaccel-node` 日志和 loadavg。
- [x] 后端新增节点丢包风险评分，按 `low`、`medium`、`high` 返回风险等级、检查项、关键指标和处理建议，避免只靠人工看 `journalctl`。
- [x] “优化 UDP Buffer”任务默认阈值提升到 64MB，并同步写入 `rmem_default`、`wmem_default` 和 `netdev_max_backlog`，适配游戏加速高峰 UDP/QUIC 突发流量。
- [x] 节点接入自检弹窗新增“网络体检”入口，管理员可在控制面板直接查看丢包风险、缓冲区、队列、网卡 dropped/error 和建议。
- [x] 本地验证通过：`go test ./internal/panel`、`go test ./...`、`web/panel npm run build`。

### 阶段 77：v0.7.2 节点主动连通性探测

- [x] 新增 `GET /api/panel/nodes/{node_id}/connectivity-probe`，从控制面板服务器主动探测节点入口 DNS、Admin TCP、Admin `/health`、QUIC 握手和临时 token Ping。
- [x] 主动探测不依赖 SSH、不修改节点配置，用于区分“节点本机参数/网卡问题”和“面板到节点入口不可达、UDP 端口未放行、HMAC Secret 不一致”等问题。
- [x] QUIC 探测复用节点 `hmac_secret` 的加密副本生成短期测试 JWT，只返回探测结果，不在响应和日志中暴露明文密钥。
- [x] 节点接入自检弹窗新增“主动探测”入口，并把主动连通性探测、SSH 网络体检、Admin 修复和 UDP Buffer 优化分区展示。
- [x] 前端版本号提升到 `0.7.2`，本地验证通过：`go test ./internal/panel`、`web/panel npm run build`。

### 阶段 78：v0.7.3 节点 UDP 丢包原因计数

- [x] 节点 UDP Datagram 转发路径新增 `udp/drop` flow event，按 `reason`、`game_id`、`policy_id` 聚合。
- [x] 覆盖 `rate_limited_client_to_target`、`rate_limited_target_to_client`、`send_queue_overflow`、`send_queue_full`、`send_datagram_failed`、`invalid_datagram`、`unsupported_datagram` 和 `unknown_flow`。
- [x] 保持客户端协议不变，仍然使用 QUIC Datagram；本阶段只增加节点侧观测和统计，不改成本地代理或可靠 UDP。

### 阶段 79：v0.7.4 面板 UDP 丢包观测

- [x] `GET /api/panel/traffic/overview` 新增 `totals.udp_packet_drops`，按窗口增量统计节点上报的 UDP 丢包事件。
- [x] 流量与联调观测页新增“UDP 丢包”指标卡和“UDP 丢包原因排行”，用于区分队列溢出、限速、未知 flow 和 datagram 发送失败。
- [x] 补充后端聚合测试和指标 collector 测试，避免历史累计值误判为当前窗口问题。

### 阶段 80：v0.7.5 UDP 发送队列配置

- [x] 节点配置新增 `limits.udp_send_queue_size`，默认 `1024`，校验范围 `16..65536`。
- [x] UDP target -> client 回包队列从固定 64 改为读取配置，队列满时保留低延迟策略：丢弃旧包并记录 `send_queue_overflow`。
- [x] 控制面板一键部署模板和 `config.example.yaml` 默认写入 `udp_send_queue_size: 1024`。

### 阶段 81：v0.7.6 Backend API Key 管理员查看

- [x] 新增 `GET /api/panel/security/backend-api-keys` 管理员接口，返回控制面板 `security.backend_api_keys` 的完整值、掩码和长度。
- [x] 接口只允许面板管理员 Bearer JWT 访问，不接受业务 Backend API Key 自身鉴权。
- [x] 查看密钥会写入 `panel_audit_logs`，审计 action 为 `panel.security.backend_api_keys.view`，日志不记录密钥明文。
- [x] 账号与权限页的 `Backend API Key` 指标增加“查看”按钮，弹窗展示完整密钥并支持复制。
- [x] 本阶段无数据库迁移；配置仍来自 Go 后端 `panel.yaml` 的 `security.backend_api_keys`。
- [x] 本地验证通过：`go test ./internal/panel`、`go test ./...`、`web/panel npm run build`。
