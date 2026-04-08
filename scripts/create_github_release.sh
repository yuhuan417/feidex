#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/create_github_release.sh <version>

Examples:
  ./scripts/create_github_release.sh 0.2.0
  ./scripts/create_github_release.sh v0.2.0

This script creates and pushes a git tag. GitHub Actions will publish the release
for tags matching v*.
EOF
}

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 1
fi

raw_version="$(printf '%s' "$1" | tr -d '[:space:]')"
[[ -n "$raw_version" ]] || fail "version must not be empty"

version="$raw_version"
if [[ "$version" != v* ]]; then
  version="v$version"
fi

if ! [[ "$version" =~ ^v[0-9]+(\.[0-9]+){2}([.-][0-9A-Za-z.-]+)?$ ]]; then
  fail "version must look like v1.2.3"
fi

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || fail "not inside a git repository"

current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$current_branch" != "main" ]]; then
  fail "current branch is '$current_branch'; switch to main before releasing"
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  fail "tracked changes are not committed"
fi

if git rev-parse "$version" >/dev/null 2>&1; then
  fail "tag '$version' already exists locally"
fi

if git ls-remote --exit-code --tags origin "refs/tags/$version" >/dev/null 2>&1; then
  fail "tag '$version' already exists on origin"
fi

git fetch origin main --tags >/dev/null 2>&1

local_head="$(git rev-parse HEAD)"
remote_head="$(git rev-parse origin/main)"
if [[ "$local_head" != "$remote_head" ]]; then
  fail "local HEAD does not match origin/main; pull or push before releasing"
fi

printf 'Creating tag %s at %s\n' "$version" "$local_head"
git tag -a "$version" -m "Release $version"

cleanup_tag() {
  if git rev-parse "$version" >/dev/null 2>&1; then
    git tag -d "$version" >/dev/null 2>&1 || true
  fi
}

trap cleanup_tag ERR

git push origin "$version"

trap - ERR

printf 'Pushed tag %s\n' "$version"
printf 'GitHub Actions will publish the release for %s\n' "$version"
