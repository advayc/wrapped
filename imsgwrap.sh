#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN="$ROOT/imsgwrap"
REMOTE_BIN_URL="https://raw.githubusercontent.com/advayc/wrapped/main/imsgwrap"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/imsgwrap"
CACHE_BIN="$CACHE_DIR/imsgwrap"

if [ -d "$ROOT/cmd/imsgwrap" ] && [ -f "$ROOT/go.mod" ] && command -v go >/dev/null 2>&1 && go version >/dev/null 2>&1; then
  exec go run "$ROOT/cmd/imsgwrap" "$@"
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
