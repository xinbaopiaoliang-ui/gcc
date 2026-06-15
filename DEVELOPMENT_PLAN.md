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

## 已知风险与注意事项

- QUIC Datagram 不保证可靠到达，适合 UDP 实时游戏包；控制消息和 TCP 必须走 Stream。
- Datagram payload 需要控制大小，避免超过路径 MTU；第一版建议客户端侧控制在 1200 字节以内。
- 服务端按客户端请求转发，但必须保留基础目标限制，否则会变成开放代理。
- 离线 token 难实时撤销，生产环境应使用短有效期 token。
- 管理 API 不应暴露到公网，除非启用强鉴权或 mTLS。
- 队列策略优先低延迟，实时 UDP 包宁愿丢弃旧包，也不要排队堆积。
