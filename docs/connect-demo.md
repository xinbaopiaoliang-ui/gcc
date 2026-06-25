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

## Steam 客户端模式

这是你要优先测的模式：测试 Steam 客户端内置商店、社区、论坛，而不是浏览器单页。

先在 Windows 托盘里完全退出 Steam，再启动：

```powershell
.\gaccel-connect-demo.exe `
  -steam-client-mode `
  -listen 127.0.0.1:18080 `
  -addr 195.245.242.9:5555 `
  -token "你的 JWT token" `
  -client-id steam-client-demo `
  -client-version 0.4.0-demo `
  -game-id steam `
  -policy-id steam-web-v1 `
  -client-config-revision 20260616.1 `
  -insecure=true
```

`-steam-client-mode` 会做三件事：

1. 启动本机 HTTP CONNECT 入口 `127.0.0.1:18080`。
2. 临时设置 Windows 当前用户系统代理为 `127.0.0.1:18080`。
3. 拉起 Steam 并打开商店。

然后在 Steam 客户端里面点击：

- 商店首页
- 社区
- 讨论/论坛
- 个人资料或创意工坊页面

成功时 demo 控制台会持续出现类似日志：

```text
connect opened target=store.steampowered.com:443
connect opened target=steamcommunity.com:443
connect opened target=community.akamai.steamstatic.com:443
```

退出 demo 时会自动恢复之前的 Windows 系统代理。

如果 Steam 已经在后台运行，它可能不会立刻读取新的系统代理；这种情况请从托盘完全退出 Steam 后重新运行上面的命令。

## 普通 CONNECT 模式

```powershell
go run ./cmd/gaccel-connect-demo `
  -listen 127.0.0.1:18080 `
  -addr 195.245.242.9:5555 `
  -token "你的 JWT token" `
  -client-id steam-connect-demo `
  -client-version 0.4.0-demo `
  -game-id steam `
  -policy-id steam-web-v1 `
  -client-config-revision 20260616.1 `
  -insecure=true
```

默认只允许以下 Steam 相关域名和 `443,27014-27050` 端口：

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
steam-chat.com
*.steam-chat.com
steamserver.net
*.steamserver.net
steamgames.com
*.steamgames.com
steam-api.com
*.steam-api.com
valvesoftware.com
*.valvesoftware.com
akamaihd.net
*.akamaihd.net
fastly.net
*.fastly.net
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

启用节点 `route_policies` 后，demo 会按 Steam 目标域名自动生成 `rule_id`，并把 `game_id`、`policy_id`、`client_config_revision` 写入 `OPEN_TCP.metadata`。如果 token 没有授权对应 `game_ids` / `policy_ids`，节点会返回 `target_denied`。

## Steam 客户端验证说明

Steam 客户端是否使用系统代理取决于它内部 WebView 和系统环境。建议顺序：

1. 完全退出 Steam。
2. 使用 `-steam-client-mode` 启动 demo。
3. Steam 自动打开后，直接在 Steam 客户端里打开商店、社区和论坛页面。
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
- Steam 客户端会访问部分 `cmp*.steamserver.net:270xx` TCP 端口；节点策略也需要放行 `.steamserver.net` 的对应端口，否则 demo 放行后仍会被节点拒绝。
- 不应把 `-listen` 改成公网地址；如果确实要调试非本机访问，必须显式加 `-allow-nonlocal-listen`。
