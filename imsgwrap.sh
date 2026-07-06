#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to run from source: https://go.dev/dl/"
  exit 1
fi

if [ -d "$ROOT/cmd/imsgwrap" ] && [ -f "$ROOT/go.mod" ]; then
  cd "$ROOT"
  exec go run ./cmd/imsgwrap "$@"
fi

PKG="${IMSGWRAP_GO_PACKAGE:-github.com/AdvayChandorkar/imsgwrap/cmd/imsgwrap@latest}"
exec go run "$PKG" "$@"
