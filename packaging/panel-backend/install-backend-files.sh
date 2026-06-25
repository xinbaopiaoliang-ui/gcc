#!/bin/sh
set -eu

DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64)
    cp "$DIR/gaccel-panel-linux-amd64" "$DIR/gaccel-panel"
    ;;
  aarch64|arm64)
    cp "$DIR/gaccel-panel-linux-arm64" "$DIR/gaccel-panel"
    ;;
  *)
    echo "unsupported arch: $ARCH" >&2
    exit 1
    ;;
esac

chmod +x "$DIR/gaccel-panel" "$DIR/start.sh"

if [ ! -f "$DIR/panel.yaml" ]; then
  cp "$DIR/panel.example.yaml" "$DIR/panel.yaml"
  echo "created $DIR/panel.yaml from panel.example.yaml"
fi

echo "gaccel-panel backend files are ready."
echo "Edit $DIR/panel.yaml, then start with:"
echo "  $DIR/gaccel-panel -config $DIR/panel.yaml"
