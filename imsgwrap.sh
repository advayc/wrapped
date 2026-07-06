#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN="$ROOT/imsgwrap"
REMOTE_BIN_URL="https://raw.githubusercontent.com/advayc/wrapped/main/imsgwrap"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/imsgwrap"
CACHE_BIN="$CACHE_DIR/imsgwrap"

if command -v go >/dev/null 2>&1; then
  if [ -d "$ROOT/cmd/imsgwrap" ] && [ -f "$ROOT/go.mod" ]; then
    cd "$ROOT"
    exec go run ./cmd/imsgwrap "$@"
  fi

  PKG="${IMSGWRAP_GO_PACKAGE:-github.com/advayc/wrapped/cmd/imsgwrap@latest}"
  exec go run "$PKG" "$@"
fi

if [ -x "$BIN" ]; then
  exec "$BIN" "$@"
fi

mkdir -p "$CACHE_DIR"
if [ ! -x "$CACHE_BIN" ]; then
  tmp="$CACHE_BIN.$$"
  curl -fsSL "$REMOTE_BIN_URL" -o "$tmp"
  chmod +x "$tmp"
  mv "$tmp" "$CACHE_BIN"
fi

exec "$CACHE_BIN" "$@"
