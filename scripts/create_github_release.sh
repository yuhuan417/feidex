#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/create_github_release.sh [version]

Examples:
  ./scripts/create_github_release.sh
  ./scripts/create_github_release.sh 0.2.0
  ./scripts/create_github_release.sh v0.2.0

If no version is provided, the script detects the latest stable tag on origin
and bumps the minor version, resetting patch to 0. For example:
  v0.4.0 -> v0.5.0
  v0.4.3 -> v0.5.0

This script creates and pushes a git tag. GitHub Actions will publish the
release for tags matching v*.
EOF
}

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

normalize_version() {
  local raw_version version
  raw_version="$(printf '%s' "${1:-}" | tr -d '[:space:]')"
  [[ -n "$raw_version" ]] || fail "version must not be empty"

  version="$raw_version"
  if [[ "$version" != v* ]]; then
    version="v$version"
  fi

  if ! [[ "$version" =~ ^v[0-9]+(\.[0-9]+){2}([.-][0-9A-Za-z.-]+)?$ ]]; then
    fail "version must look like v1.2.3"
  fi

  printf '%s\n' "$version"
}

latest_stable_remote_tag() {
  git ls-remote --tags --refs origin 'v*' \
    | awk '{print $2}' \
    | sed 's#^refs/tags/##' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V \
    | tail -n 1
}

auto_detect_next_version() {
  local latest stripped major minor next_minor
  latest="$(latest_stable_remote_tag)"
  if [[ -z "$latest" ]]; then
    printf 'v0.1.0\n'
    return
  fi

  stripped="${latest#v}"
  IFS='.' read -r major minor _ <<<"$stripped"
  next_minor=$((minor + 1))
  printf 'v%s.%s.0\n' "$major" "$next_minor"
}

if [[ $# -gt 1 ]]; then
  usage >&2
  exit 1
fi

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || fail "not inside a git repository"

current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$current_branch" != "main" ]]; then
  fail "current branch is '$current_branch'; switch to main before releasing"
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  fail "tracked changes are not committed"
fi

git fetch origin main --tags >/dev/null 2>&1

if [[ $# -eq 1 ]]; then
  version="$(normalize_version "$1")"
else
  version="$(auto_detect_next_version)"
  printf 'Auto-detected version %s\n' "$version"
fi

if git rev-parse "$version" >/dev/null 2>&1; then
  fail "tag '$version' already exists locally"
fi

if git ls-remote --exit-code --tags origin "refs/tags/$version" >/dev/null 2>&1; then
  fail "tag '$version' already exists on origin"
fi

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
