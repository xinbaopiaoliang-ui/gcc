#!/usr/bin/env sh
set -eu

REPO="${REPO:-xinbaopiaoliang-ui/gcc}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${CONFIG_DIR:-/etc/gaccel-node}"
STATE_DIR="${STATE_DIR:-/var/lib/gaccel-node}"
SERVICE_PATH="${SERVICE_PATH:-/etc/systemd/system/gaccel-node.service}"
USER_NAME="${USER_NAME:-gaccel}"
GROUP_NAME="${GROUP_NAME:-gaccel}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_root() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "please run as root" >&2
    exit 1
  fi
}

detect_nologin() {
  if [ -x /usr/sbin/nologin ]; then
    echo /usr/sbin/nologin
  elif [ -x /sbin/nologin ]; then
    echo /sbin/nologin
  else
    echo /bin/false
  fi
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
  esac
}

download() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    echo "missing required command: curl or wget" >&2
    exit 1
  fi
}

need_root
need_cmd uname
need_cmd tar
need_cmd sha256sum

ARCH="$(detect_arch)"
if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
  DISPLAY_VERSION="latest"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
  DISPLAY_VERSION="$VERSION"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ARCHIVE_PATTERN="gaccel-node_*_linux-${ARCH}.tar.gz"
CHECKSUMS="${TMP_DIR}/SHA256SUMS"

echo "Downloading gaccel-node ${DISPLAY_VERSION} for linux-${ARCH} from ${REPO}"
download "${BASE_URL}/SHA256SUMS" "$CHECKSUMS"

ARCHIVE_NAME="$(grep "linux-${ARCH}.tar.gz" "$CHECKSUMS" | awk '{print $2}' | head -n 1)"
if [ -z "$ARCHIVE_NAME" ]; then
  echo "cannot find linux-${ARCH} archive in SHA256SUMS" >&2
  exit 1
fi

case "$ARCHIVE_NAME" in
  $ARCHIVE_PATTERN) ;;
  *)
    echo "unexpected archive name: $ARCHIVE_NAME" >&2
    exit 1
    ;;
esac

download "${BASE_URL}/${ARCHIVE_NAME}" "${TMP_DIR}/${ARCHIVE_NAME}"
(cd "$TMP_DIR" && grep " ${ARCHIVE_NAME}$" SHA256SUMS | sha256sum -c -)

tar -C "$TMP_DIR" -xzf "${TMP_DIR}/${ARCHIVE_NAME}"
PKG_DIR="$(find "$TMP_DIR" -maxdepth 1 -type d -name "gaccel-node_*_linux-${ARCH}" | head -n 1)"
if [ -z "$PKG_DIR" ]; then
  echo "cannot find unpacked package directory" >&2
  exit 1
fi

if ! getent group "$GROUP_NAME" >/dev/null 2>&1; then
  groupadd --system "$GROUP_NAME"
fi

if ! id "$USER_NAME" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell "$(detect_nologin)" --gid "$GROUP_NAME" "$USER_NAME"
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$STATE_DIR"
install -m 0755 "${PKG_DIR}/gaccel-node" "${INSTALL_DIR}/gaccel-node"
install -m 0755 "${PKG_DIR}/gaccel-probe" "${INSTALL_DIR}/gaccel-probe"
if [ -f "${PKG_DIR}/gaccel-token" ]; then
  install -m 0755 "${PKG_DIR}/gaccel-token" "${INSTALL_DIR}/gaccel-token"
fi
if [ -f "${PKG_DIR}/gaccel-token-api" ]; then
  install -m 0755 "${PKG_DIR}/gaccel-token-api" "${INSTALL_DIR}/gaccel-token-api"
fi

if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
  install -m 0644 "${PKG_DIR}/config.example.yaml" "${CONFIG_DIR}/config.yaml"
  sed -i "s|cert_file: \"./cert.pem\"|cert_file: \"${CONFIG_DIR}/cert.pem\"|" "${CONFIG_DIR}/config.yaml"
  sed -i "s|key_file: \"./key.pem\"|key_file: \"${CONFIG_DIR}/key.pem\"|" "${CONFIG_DIR}/config.yaml"
fi

if [ -f "${PKG_DIR}/token-api.example.yaml" ] && [ ! -f "${CONFIG_DIR}/token-api.yaml" ]; then
  install -m 0640 "${PKG_DIR}/token-api.example.yaml" "${CONFIG_DIR}/token-api.yaml"
  chown "root:${GROUP_NAME}" "${CONFIG_DIR}/token-api.yaml"
fi

chown -R "${USER_NAME}:${GROUP_NAME}" "$STATE_DIR"
install -m 0644 "${PKG_DIR}/deployments/gaccel-node.service" "$SERVICE_PATH"
if [ -f "${PKG_DIR}/deployments/gaccel-token-api.service" ]; then
  install -m 0644 "${PKG_DIR}/deployments/gaccel-token-api.service" "/etc/systemd/system/gaccel-token-api.service"
fi

systemctl daemon-reload
systemctl enable gaccel-node

echo "Installed gaccel-node."
echo "Edit ${CONFIG_DIR}/config.yaml and put TLS files at ${CONFIG_DIR}/cert.pem / ${CONFIG_DIR}/key.pem."
echo "Then start it with: systemctl start gaccel-node"
echo "Optional token API config: ${CONFIG_DIR}/token-api.yaml, start with: systemctl start gaccel-token-api"
echo "Open UDP 443 in your firewall/security group."
