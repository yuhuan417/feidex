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
  FEIDEX_GOCACHE
  FEIDEX_GOMODCACHE
EOF
}

if [[ $# -eq 0 ]]; then
  usage >&2
  exit 1
fi

export GOCACHE="${FEIDEX_GOCACHE:-/tmp/feidex-gocache}"
export GOMODCACHE="${FEIDEX_GOMODCACHE:-/tmp/feidex-gomodcache}"

mkdir -p "$GOCACHE" "$GOMODCACHE"

exec "$@"
