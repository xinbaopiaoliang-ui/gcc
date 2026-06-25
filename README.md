# gaccel-node

通用游戏加速 QUIC Relay 服务端内核。

当前范围：

- 只做服务端节点内核。
- 不做本地客户端产品。
- 不做 VPN 下发。
- 不做 V2bX 面板拉节点。
- 服务端按客户端连接请求执行通用 TCP/UDP 转发。

## 快速开始

### GitHub Release 安装

服务器上一键安装最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh | sudo sh
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh | sudo env VERSION=v0.3.2 sh
```

安装后编辑：

```bash
sudo nano /etc/gaccel-node/config.yaml
```

准备 TLS 证书：

```bash
sudo openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout /etc/gaccel-node/key.pem \
  -out /etc/gaccel-node/cert.pem \
  -days 365 \
  -subj "/CN=your-domain-or-ip"
```

启动服务：

```bash
sudo systemctl start gaccel-node
sudo systemctl status gaccel-node
```

### 源码启动

复制示例配置：

```powershell
Copy-Item config.example.yaml config.yaml
```

准备 TLS 证书后启动：

```powershell
go run ./cmd/server -config config.yaml
```

管理接口默认监听：

```text
http://127.0.0.1:9090/health
http://127.0.0.1:9090/status
http://127.0.0.1:9090/sessions
http://127.0.0.1:9090/panel/commands
http://127.0.0.1:9090/metrics
POST http://127.0.0.1:9090/config/reload
http://127.0.0.1:9090/debug/pprof/
```

`/sessions` 会输出在线连接、flow、user_id、device_id、client_id、client_version、client_platform、last_ping_at、有效连接上限、有效限速和 TCP/UDP 权限，方便客户端联调和后续面板侧排查节点状态。
`/panel/commands` 会输出最近的面板命令执行结果，包含成功/失败、错误信息和 `stage_upgrade` 暂存包详情。

## 测试工具

`gaccel-probe` 仅用于协议验证，不是正式客户端。

```powershell
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode ping -client-id dev-client -client-version 0.3.1 -client-platform windows/amd64
```

保活联调：

```powershell
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode keepalive -count 4 -interval 15s -timeout 70s -client-id dev-client
```

## Token 获取接口

`gaccel-token-api` 可以由面板或后端调用，用服务端保存的 `hmac_secret` 签发短期 token。客户端只拿 token 连接节点，不持有 `hmac_secret`。

```bash
gaccel-token-api -config token-api.example.yaml
```

签发示例：

```bash
curl -sS http://127.0.0.1:8088/token \
  -H "Authorization: Bearer your-token-api-key" \
  -H "Content-Type: application/json" \
  -d '{"user_id":"dev","device_id":"win-test","ttl_seconds":3600}'
```

## HMAC Token 鉴权

生产测试建议把 `auth.mode` 改为 `hmac`，并设置足够长的随机密钥：

```yaml
auth:
  mode: "hmac"
  hmac_secret: "replace-with-a-long-random-secret"
  token_leeway: "30s"
```

生成短期 token：

```bash
gaccel-token -secret "replace-with-a-long-random-secret" -user user-1 -device device-1 -ttl 15m -max-connections 2 -rate-limit-mbps 50 -game-ids steam -policy-ids steam-web-v1 -config-revision 20260616.1
```

源码运行时也可以这样生成：

```bash
go run ./cmd/gaccel-token -secret "replace-with-a-long-random-secret" -user user-1 -ttl 15m -game-ids steam -policy-ids steam-web-v1 -config-revision 20260616.1
```

配置修改后热重载：

```bash
curl -X POST http://127.0.0.1:5557/config/reload
```

## 节点元数据

节点配置可填写面板识别用的元数据：

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
```

`GET /status` 会返回 `node` 字段，后续面板可用它做节点展示、筛选和区域调度。

## 面板上报

节点可选开启到面板的 heartbeat/report，只 POST 状态，不接收配置下发：

```yaml
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

upgrade:
  stage_dir: "/var/lib/gaccel-node/upgrades"
  max_package_bytes: 209715200
  timeout: "2m"
  allow_http: false
```

上报内容包含 `status`、节点版本、时间戳、`node` 元数据、QUIC 监听信息和指标快照。`panel.report_url` 为空时不会启动上报。`panel.command_url` 可选开启签名运维命令拉取，当前支持 `noop`、`config_reload`、`apply_config`、`apply_policy` 和 `stage_upgrade`。

`stage_upgrade` 只下载并校验升级包，把文件暂存在 `upgrade.stage_dir`，不会直接替换正在运行的二进制，也不会自动重启节点。

## 当前状态

已完成项目骨架、配置加载、管理 health/status/sessions/metrics/config reload/pprof 接口、QUIC Listener、Control Stream、HELLO/AUTH/PING、UDP Datagram Relay、TCP Stream Relay、HMAC/JWT 短期 token、基础在线统计、用户级流量统计、flow 原因统计、TCP 关闭通知和测试模拟工具。

当前优先级是 `v0.3.2` 客户端联调与节点面板对接：提供 Rust 联调文档、Steam 页面 demo、节点元数据和面板 heartbeat/report。监听地址、TLS 证书和 QUIC listener 级参数仍需要重启进程才能完全生效。

## Windows Steam Demo

GitHub Release 会提供 Windows 页面联调包：

```text
gaccel-steam-demo_<version>_windows-amd64.zip
```

解压后双击 `gaccel-steam-demo.exe`，即可在本地页面填写节点、Token 和 Steam 社区目标进行 QUIC 原生转发测试。

## Windows CONNECT Demo

GitHub Release 会提供 Windows HTTP CONNECT 联调包：

```text
gaccel-connect-demo_<version>_windows-amd64.zip
```

它不是正式客户端，只是给客户端团队验证真实 Steam 商店/论坛 HTTPS 流量的参考工具。`-steam-client-mode` 会临时设置 Windows 当前用户系统代理并拉起 Steam，让 Steam 客户端内置商店/社区页面通过 `127.0.0.1:18080` 发起 HTTP CONNECT，demo 再通过 QUIC `OPEN_TCP` 转发到节点。

启动示例：

```powershell
gaccel-connect-demo.exe -steam-client-mode -listen 127.0.0.1:18080 -addr 195.245.242.9:5555 -token "你的 JWT token" -insecure=true
```

如果只想用浏览器验证 CONNECT 行为：

```powershell
Start-Process msedge.exe "--proxy-server=http://127.0.0.1:18080 https://store.steampowered.com/"
Start-Process msedge.exe "--proxy-server=http://127.0.0.1:18080 https://steamcommunity.com/discussions/"
```

详细说明见 [HTTP CONNECT QUIC 联调 Demo](./docs/connect-demo.md)。

## 文档

- [开发计划](./DEVELOPMENT_PLAN.md)
- [协议草案](./docs/protocol.md)
- [Token 获取接口](./docs/token-api.md)
- [客户端与节点联调协议说明](./docs/client-node-integration.md)
- [Rust 客户端联调指南](./docs/rust-client.md)
- [HTTP CONNECT QUIC 联调 Demo](./docs/connect-demo.md)
- [业务后台游戏配置与节点策略设计](./docs/backend-game-config.md)
- [面板对接协议](./docs/panel.md)
- [部署说明](./docs/deploy.md)

## 发布新版本

推送 `v*` tag 会触发 GitHub Release 自动打包：

```bash
git tag v0.3.2
git push origin v0.3.2
```
