# 部署说明

## GitHub Release 一键安装

默认仓库：

```text
https://github.com/xinbaopiaoliang-ui/gcc
```

安装最新版本：

```bash
curl -fsSL https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh | sudo sh
```

安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh | sudo env VERSION=v0.3.2 sh
```

安装脚本会：

- 下载 GitHub Release 中匹配当前 Linux 架构的包。
- 校验 `SHA256SUMS`。
- 安装 `gaccel-node`、`gaccel-probe`、`gaccel-token` 和 `gaccel-token-api` 到 `/usr/local/bin`。
- 初始化 `/etc/gaccel-node/config.yaml`。
- 初始化 `/etc/gaccel-node/token-api.yaml`，但不会默认启动 token API。
- 安装 systemd service。

安装后需要准备 TLS 证书，并启动服务：

```bash
sudo openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout /etc/gaccel-node/key.pem \
  -out /etc/gaccel-node/cert.pem \
  -days 365 \
  -subj "/CN=your-domain-or-ip"

sudo systemctl start gaccel-node
sudo systemctl status gaccel-node
```

可选填写节点元数据，便于后续面板识别和分组：

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

修改后可通过管理接口确认：

```bash
curl http://127.0.0.1:5557/status
curl http://127.0.0.1:5557/panel/commands
```

## 面板状态上报

节点可以主动向面板上报状态：

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

该功能只做 heartbeat/report：节点定时 POST 自身状态、版本、节点元数据和指标快照，不接收面板配置下发，也不要求把管理 API 暴露到公网。

建议面板侧校验 `Authorization: Bearer <api_key>`，并记录节点 `node.id`、`version`、`timestamp`、`metrics.active_quic_connections` 等字段。

如需下发运维命令，配置 `command_url` 和 `command_secret`。节点会主动拉取并校验 HMAC 签名，当前支持 `noop`、`config_reload`、`apply_config` 和 `stage_upgrade`，详见 `docs/panel.md`。

`stage_upgrade` 只负责安全暂存升级包：下载、校验 SHA256、写入 manifest。它不会替换 `/usr/local/bin/gaccel-node`，也不会自动重启服务。

## systemd

示例 unit 文件位于 `deployments/gaccel-node.service`。

建议目录：

```text
/usr/local/bin/gaccel-node
/etc/gaccel-node/config.yaml
/etc/gaccel-node/cert.pem
/etc/gaccel-node/key.pem
/etc/gaccel-node/token-api.yaml
```

安装后：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gaccel-node
sudo systemctl status gaccel-node
```

Token API 可选启动：

```bash
sudo nano /etc/gaccel-node/token-api.yaml
sudo systemctl start gaccel-token-api
sudo systemctl status gaccel-token-api --no-pager
```

## Docker

构建镜像：

```bash
docker build -t gaccel-node:dev .
```

运行示例：

```bash
docker run --rm \
  -p 443:443/udp \
  -p 127.0.0.1:9090:9090/tcp \
  -v /etc/gaccel-node:/app:ro \
  gaccel-node:dev
```

## 日志轮转

当前服务默认输出到 stdout。

- systemd 部署时优先使用 journald 管理日志。
- Docker 部署时使用 Docker logging driver 管理日志。
- 如果后续改为文件日志，建议按大小轮转并保留 7-14 天。

systemd 可用以下命令查看日志：

```bash
journalctl -u gaccel-node -f
```

## pprof

pprof 挂在管理接口上，默认仅监听本机地址：

```text
http://127.0.0.1:9090/debug/pprof/
```

不要把管理接口直接暴露到公网。

## 配置热重载

```bash
curl -X POST http://127.0.0.1:9090/config/reload
```

热重载会更新新鉴权、新连接、新 flow 使用的运行时策略。监听地址、TLS 证书和 QUIC listener 级参数需要重启进程才能完全生效。

## Windows Steam Demo

GitHub Release 附带 Windows 页面联调包：

```text
gaccel-steam-demo_<version>_windows-amd64.zip
```

该包用于本地页面测试 Steam Community 是否能通过 QUIC 节点访问，不会安装到 Linux 服务器，也不会修改 Windows 系统代理。
