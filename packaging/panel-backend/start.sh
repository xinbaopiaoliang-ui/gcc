#!/bin/sh
set -eu

DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$DIR/gaccel-panel" -config "$DIR/panel.yaml"
