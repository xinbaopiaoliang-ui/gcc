gaccel-panel frontend v0.6.23

Deploy these files to the PHP/HTML site root, for example:
  103.201.131.99:9788

This package is frontend-only. Do not unzip it into the Go backend directory.

Runtime API base:
  Edit panel-config.js if your Go backend API address changes.

Default API base in this build:
  http://103.201.131.99:8091

The browser calls the Go backend with:
  Authorization: Bearer <panel_access_token>

The Go backend must allow the frontend origin in panel.yaml:
  cors:
    allowed_origins:
      - "http://103.201.131.99:9788"

After login, administrators can open the system self-check page from the left sidebar.

v0.6.1:
  Node access check includes "Repair Admin Access" for fixing node admin listen
  from the panel after SSH credentials are saved.

v0.6.2:
  Repair Admin Access uses a softer dark-blue action button.

v0.6.4-v0.6.6:
  Deploy dialog no longer asks operators to enter node HMAC Secret. It shows whether
  the business backend has synced the secret, while the Go backend reads the encrypted
  node copy during deployment.

v0.6.7:
  Node access check adds "Optimize UDP Buffer". It creates a backend task that tunes
  net.core.rmem_max and net.core.wmem_max to 16777216 on the node through SSH.

v0.6.8:
  The System page adds "Client Session Defaults" for editing the default token
  max_connections and rate_limit_mbps profiles used by the business backend.

v0.6.20:
  The left sidebar adds "Client Sessions". It shows when users connect to a node,
  when authentication succeeds, when the session ends, close reason, latest ping,
  game/policy IDs, TCP/UDP flow counts, and session traffic.

v0.6.21:
  Fixes page-level horizontal drift caused by the fixed sidebar offset. The main
  content width is constrained on desktop while table-level horizontal scrolling
  remains available.

v0.6.22:
  Fixes wide-screen content alignment after v0.6.21. Main pages now fill the
  available panel content area instead of centering inside a 1440px container.

v0.6.23:
  Fixes grid page overflow on the Traffic and Client Sessions pages. Their grid
  containers now use minmax(0, 1fr), so inner cards and tables cannot expand the
  page wider than the panel content area.
