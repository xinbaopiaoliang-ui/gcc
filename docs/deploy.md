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
curl -fsSL https://raw.githubusercontent.com/xinbaopiaoliang-ui/gcc/main/scripts/install.sh | sudo env VERSION=v0.1.0 sh
```

安装脚本会：

- 下载 GitHub Release 中匹配当前 Linux 架构的包。
- 校验 `SHA256SUMS`。
- 安装 `gaccel-node` 和 `gaccel-probe` 到 `/usr/local/bin`。
- 初始化 `/etc/gaccel-node/config.yaml`。
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

## systemd

示例 unit 文件位于 `deployments/gaccel-node.service`。

建议目录：

```text
/usr/local/bin/gaccel-node
/etc/gaccel-node/config.yaml
/etc/gaccel-node/cert.pem
/etc/gaccel-node/key.pem
```

安装后：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gaccel-node
sudo systemctl status gaccel-node
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
