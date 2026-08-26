#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/with_tmp_go_cache.sh <command> [args...]

Examples:
  ./scripts/with_tmp_go_cache.sh go test ./internal/app
  ./scripts/with_tmp_go_cache.sh go test ./...
  ./scripts/with_tmp_go_cache.sh go build -o bin/feidex ./cmd/feidex

Environment overrides:
  FEIDEX_CACHE_HOME
  FEIDEX_GOCACHE
  FEIDEX_GOMODCACHE
EOF
}

if [[ $# -eq 0 ]]; then
  usage >&2
  exit 1
fi

cache_home="${FEIDEX_CACHE_HOME:-${XDG_CACHE_HOME:-$HOME/.cache}/feidex}"

export GOCACHE="${FEIDEX_GOCACHE:-$cache_home/go-build}"
export GOMODCACHE="${FEIDEX_GOMODCACHE:-$cache_home/gomodcache}"

mkdir -p "$GOCACHE" "$GOMODCACHE"

exec "$@"
