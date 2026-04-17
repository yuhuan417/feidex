#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/clean_tmp_go_cache.sh

Environment overrides:
  FEIDEX_GOCACHE
  FEIDEX_GOMODCACHE
EOF
}

if [[ $# -ne 0 ]]; then
  usage >&2
  exit 1
fi

gocache="${FEIDEX_GOCACHE:-/tmp/feidex-gocache}"
gomodcache="${FEIDEX_GOMODCACHE:-/tmp/feidex-gomodcache}"

for path in "$gocache" "$gomodcache"; do
  if [[ -e "$path" ]]; then
    chmod -R u+w "$path" 2>/dev/null || true
    rm -rf "$path"
  fi
done
