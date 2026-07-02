gaccel-panel Go backend v0.7.13

Unzip this package under:
  /www/wwwroot/go

Expected path after unzip:
  /www/wwwroot/go/gaccel-panel

First-time preparation:
  cd /www/wwwroot/go/gaccel-panel
  sh install-backend-files.sh
  cp panel.example.yaml panel.yaml
  vi panel.yaml

Baota Go project example:
  Project executable:
    /www/wwwroot/go/gaccel-panel/gaccel-panel
  Project port:
    The port configured in panel.yaml listen. If Baota asks for the real port, use the port part only.
  Execute command:
    ./gaccel-panel -config /www/wwwroot/go/gaccel-panel/panel.yaml
  Working directory:
    /www/wwwroot/go/gaccel-panel

Important:
  - Do not put frontend files into this Go backend directory.
  - Frontend static files should be deployed to the PHP/HTML site, for example 103.201.131.99:9788.
  - The backend uses Bearer JWT for panel login and Backend API Key for business-backend/node APIs.
  - Secrets are configured in panel.yaml or environment variables and are never shown by the API.

Create or reset admin:
  ./gaccel-panel -config /www/wwwroot/go/gaccel-panel/panel.yaml -create-admin -admin-username 718886098 -admin-password 'your-password'

Health:
  curl http://127.0.0.1:<listen_port>/health

System check:
  curl http://127.0.0.1:<listen_port>/api/backend/system/check -H "Authorization: Bearer <backend_api_key>"

v0.6.1 admin repair:
  In the frontend, open Nodes -> Access Check -> Repair Admin Access.
  The backend will use the saved SSH credential to back up /etc/gaccel-node/config.yaml,
  set admin.listen, restart gaccel-node, and verify /health and /status.

v0.6.2 repair diagnostics:
  Local admin verification retries before failing. If it still fails, task logs include
  systemctl status, recent gaccel-node journal lines, and listening TCP ports.

v0.6.3 install diagnostics:
  Deploy/update tasks print the GitHub release base URL, SHA256SUMS URL, selected archive,
  and archive probe result before running the installer. Node update version must be a
  real gaccel-node GitHub Release version, not the control-panel package version.

v0.6.4-v0.6.6 backend sync:
  Business backend should generate and store each node hmac_secret, then sync it through
  POST/PUT /api/backend/nodes. The panel stores only an encrypted copy and deploy tasks
  no longer require operators to type the secret. Node sync status exposes
  hmac_secret_configured and deploy_ready. Node diagnostics also probes /sessions for
  client session and flow observability.

v0.6.7 UDP buffer tuning:
  Node diagnostics includes an "Optimize UDP Buffer" action. It uses the saved SSH
  credential to write /etc/sysctl.d/99-gaccel-quic.conf with
  net.core.rmem_max=16777216 and net.core.wmem_max=16777216, applies sysctl,
  restarts gaccel-node, checks local /health and /status, and verifies recent journal
  output no longer contains the quic-go UDP buffer warning.

v0.6.8 token defaults:
  Import migrations/20260622_v068_token_defaults.sql before using this version on an
  existing database. The panel stores default token profiles in panel_token_defaults:
  trial=32, standard=64, advanced=128, premium=256, node hard limit=512.
  Administrators can edit them from the System page. Business backend can read them with:
    GET /api/backend/token-defaults

v0.6.20 client sessions:
  Import migrations/20260623_v0616_client_sessions.sql before using this version on an
  existing database. The panel stores client connection lifecycle records in:
    panel_client_sessions
    panel_client_session_events
  Panel users can read them with:
    GET /api/panel/client-sessions
  Business backend can read them with:
    GET /api/backend/client-sessions

v0.7.1-v0.7.2 node diagnostics:
  The panel includes traffic/session troubleshooting and active node connectivity
  probing. From the node diagnostics drawer, "Active Probe" checks DNS/IP reachability,
  Admin TCP, Admin /health, QUIC handshake, and a temporary HMAC-authenticated ping.
  This probe is read-only and does not expose node hmac_secret in API responses or logs.

v0.7.13 node load metrics:
  Nodes report system load in raw_json.system, including CPU, memory, disk and network
  rate. The panel reads the latest report and exposes latest_system in node list/detail.
  Existing databases do not need a new migration for this feature.
