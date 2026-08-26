#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/clean_tmp_go_cache.sh

Environment overrides:
  FEIDEX_CACHE_HOME
  FEIDEX_GOCACHE
  FEIDEX_GOMODCACHE
EOF
}

if [[ $# -ne 0 ]]; then
  usage >&2
  exit 1
fi

cache_home="${FEIDEX_CACHE_HOME:-${XDG_CACHE_HOME:-$HOME/.cache}/feidex}"

gocache="${FEIDEX_GOCACHE:-$cache_home/go-build}"
gomodcache="${FEIDEX_GOMODCACHE:-$cache_home/gomodcache}"

for path in \
  "$gocache" \
  "$gomodcache" \
  /tmp/feidex-gocache \
  /tmp/feidex-gomodcache; do
  if [[ -e "$path" ]]; then
    chmod -R u+w "$path" 2>/dev/null || true
    rm -rf "$path"
  fi
done
