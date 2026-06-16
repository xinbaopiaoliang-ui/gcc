gaccel Steam QUIC Demo
======================

这个包是页面控制版联调 demo，不是 SOCKS5、TUN、VPN，也不会修改系统代理。

启动方式
--------
双击 gaccel-steam-demo.exe。

程序会启动一个只监听 127.0.0.1 的 Web 控制台，并自动打开浏览器页面。
配置文件默认保存在当前目录：

steam-demo-config.json

页面能做什么
------------
1. 编辑节点地址、ALPN、SNI、TLS 校验开关。
2. 编辑 Token API 地址、API Key、user_id、device_id、TTL。
3. 申请短期 JWT Token。
4. 手动粘贴 JWT Token。
5. 编辑 Steam 社区测试目标，默认是 steamcommunity.com:443。
6. 通过 QUIC HELLO / AUTH / OPEN_TCP 测试 Steam 社区 HTTPS。
7. 查看链路日志、HTTP 状态码、延迟、Content-Type 和响应预览。
8. 保存和加载本地 JSON 配置。

重要说明
--------
Token API Key 不是节点 JWT Token。
运行测试时必须使用 POST /token 返回的 token 字段，通常以 eyJ 开头，并包含两个点。

如果 Token API 只监听服务器本机 127.0.0.1:8088，Windows 本机无法直接访问。
这种情况下可以先在服务器上 curl 获取 token，再粘贴到页面的 JWT Token 输入框。

成功标志
--------
结果区显示：

HTTP 状态: 200 OK
日志里出现：
authenticated
tcp flow opened
tls established

响应预览里应能看到：
Steam Community
