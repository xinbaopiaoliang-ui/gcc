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
curl -fsSL https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh | sudo env VERSION=v0.1.0 sh
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
http://127.0.0.1:9090/metrics
POST http://127.0.0.1:9090/config/reload
http://127.0.0.1:9090/debug/pprof/
```

`/sessions` 会输出在线连接、flow、user_id、device_id、有效连接上限、有效限速和 TCP/UDP 权限，方便后续面板侧排查节点状态。

## 测试工具

`gaccel-probe` 仅用于协议验证，不是正式客户端。

```powershell
go run ./cmd/gaccel-probe -addr 127.0.0.1:443 -token dev-token -mode ping
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
gaccel-token -secret "replace-with-a-long-random-secret" -user user-1 -device device-1 -ttl 15m -max-connections 2 -rate-limit-mbps 50
```

源码运行时也可以这样生成：

```bash
go run ./cmd/gaccel-token -secret "replace-with-a-long-random-secret" -user user-1 -ttl 15m
```

配置修改后热重载：

```bash
curl -X POST http://127.0.0.1:5557/config/reload
```

## 当前状态

已完成项目骨架、配置加载、管理 health/status/sessions/metrics/config reload/pprof 接口、QUIC Listener、Control Stream、HELLO/AUTH/PING 基础协议、UDP Datagram Relay、TCP Stream Relay、HMAC/JWT 短期 token、基础在线统计、用户级流量统计、flow 原因统计、TCP 关闭通知和测试模拟工具。

当前服务端节点内核的基础链路、正式鉴权和部署打包已经完成。下一阶段进入节点运维与面板对接；监听地址、TLS 证书和 QUIC listener 级参数仍需要重启进程才能完全生效。

## 文档

- [开发计划](./DEVELOPMENT_PLAN.md)
- [协议草案](./docs/protocol.md)
- [部署说明](./docs/deploy.md)

## 发布新版本

推送 `v*` tag 会触发 GitHub Release 自动打包：

```bash
git tag v0.1.0
git push origin v0.1.0
```
