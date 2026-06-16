gaccel-connect-demo
===================

This is a small integration demo, not the final client.

It starts a local HTTP CONNECT proxy and forwards allowed Steam HTTPS
targets to gaccel-node over the native QUIC protocol.

Quick start:

1. Fully exit Steam from the Windows tray.

2. Start Steam client mode:

   gaccel-connect-demo.exe -steam-client-mode -listen 127.0.0.1:18080 -addr 195.245.242.9:5555 -token "YOUR_JWT_TOKEN" -insecure=true

3. Steam will be launched. Open Store, Community, Discussions, Workshop, or profile pages inside Steam.

4. Watch for logs:

   connect opened target=store.steampowered.com:443
   connect opened target=steamcommunity.com:443

The demo temporarily sets the current user's Windows system proxy and restores
the previous proxy when the demo exits.

Browser-only check:

   msedge.exe --proxy-server=http://127.0.0.1:18080 https://store.steampowered.com/
   msedge.exe --proxy-server=http://127.0.0.1:18080 https://steamcommunity.com/discussions/

curl check:

   curl.exe -x http://127.0.0.1:18080 https://store.steampowered.com/ -I
   curl.exe -x http://127.0.0.1:18080 https://steamcommunity.com/discussions/ -I

Default safety limits:

- listens only on 127.0.0.1
- allows only Steam-related hostnames
- allows only port 443
- uses QUIC to the node; it is not SOCKS5, TUN, or VPN

If Steam itself does not produce any "connect opened" logs, it is not using
the system/browser proxy in your current environment. In that case, use this
demo as protocol reference for the Rust client integration layer.
