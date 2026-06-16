# HTTP CONNECT QUIC 联调 Demo

`gaccel-connect-demo` 是给客户端联调用的最小参考工具，不是正式客户端产品。

它在本机启动一个 HTTP CONNECT 代理入口，浏览器或 Steam WebView 发出的 HTTPS CONNECT 会被转换为节点协议里的 `OPEN_TCP`，再通过 QUIC 发到节点。

链路：

```text
浏览器 / Steam WebView
  -> 127.0.0.1:18080 HTTP CONNECT
  -> gaccel-connect-demo
  -> QUIC gaccel/1
  -> gaccel-node
  -> Steam 商店 / 论坛 / CDN
```

节点链路仍然是 QUIC，不是 SOCKS5，不是 VPN，也不是系统 TUN。

## 启动

```powershell
go run ./cmd/gaccel-connect-demo `
  -listen 127.0.0.1:18080 `
  -addr 195.245.242.9:5555 `
  -token "你的 JWT token" `
  -client-id steam-connect-demo `
  -client-version 0.4.0-demo `
  -insecure=true
```

默认只允许以下 Steam 相关域名和 443 端口：

```text
steamcommunity.com
*.steamcommunity.com
steampowered.com
*.steampowered.com
steamstatic.com
*.steamstatic.com
steamusercontent.com
*.steamusercontent.com
steamcontent.com
*.steamcontent.com
akamaihd.net
*.akamaihd.net
```

如需临时补充域名：

```powershell
go run ./cmd/gaccel-connect-demo `
  -addr 195.245.242.9:5555 `
  -token "你的 JWT token" `
  -allowed-hosts "steamcommunity.com,.steamcommunity.com,steampowered.com,.steampowered.com,example.com" `
  -insecure=true
```

## 浏览器验证

先用浏览器验证真实页面：

```powershell
Start-Process msedge.exe "--proxy-server=http://127.0.0.1:18080 https://store.steampowered.com/"
Start-Process msedge.exe "--proxy-server=http://127.0.0.1:18080 https://steamcommunity.com/discussions/"
```

也可以用 curl 验证 CONNECT：

```powershell
curl.exe -x http://127.0.0.1:18080 https://store.steampowered.com/ -I
curl.exe -x http://127.0.0.1:18080 https://steamcommunity.com/discussions/ -I
```

成功时 demo 日志会出现：

```text
connect opened target=store.steampowered.com:443
connect opened target=steamcommunity.com:443
```

节点 `/sessions` 会看到 `client_id=steam-connect-demo`，并出现 TCP flow。

## Steam 客户端验证

Steam 客户端是否使用系统代理取决于它内部 WebView 和系统环境。建议顺序：

1. 先用 Edge/Chrome 验证 `store.steampowered.com` 和 `steamcommunity.com/discussions/`。
2. 打开 Windows 系统代理，地址填 `127.0.0.1`，端口填 `18080`。
3. 重启 Steam，打开商店和论坛页面。
4. 如果 demo 没有任何 `connect opened` 日志，说明 Steam 当前没有走系统代理，需要 Rust 客户端后续做更底层的本地流量接入。

这个 demo 的价值是给 Rust 客户端确认：

- QUIC 连接和鉴权流程。
- `OPEN_TCP` 流程。
- HTTPS CONNECT 之后的原始字节双向转发。
- 真实 Steam 商店、论坛和 CDN 域名的连接行为。

## 安全边界

- 默认只监听 `127.0.0.1`。
- 默认只允许 Steam 相关域名。
- 默认只允许 443 端口。
- 不做 DNS 劫持、不做 TUN、不做 VPN、不接管系统流量。
- 不应把 `-listen` 改成公网地址；如果确实要调试非本机访问，必须显式加 `-allow-nonlocal-listen`。
