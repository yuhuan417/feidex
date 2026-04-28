#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/test_internal_app_refactor_guard.sh

Runs the baseline verification required before and during the internal/app
refactor:
  1. Full repository tests
  2. Focused internal/app guard suite covering lifecycle, state-machine,
     backend-selection, direct-command/menu parity, and queued follow-up flows
EOF
}

if [[ $# -ne 0 ]]; then
  usage >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

guard_regex='Test(.*CriticalPath.*|.*StateMachine.*|.*MenuCommandDirectAccess.*|.*BackendSelection.*|.*SubmissionQueueClaude.*|.*CodexTurnRecovery.*|.*ServerRequest.*|.*ItemStarted.*|.*ApprovalActionWaitsForServerRequestResolved.*|.*PermissionsApprovalLifecycleResumesOnlyAfterServerRequestResolved.*|.*McpElicitationURLLifecycleResumesOnlyAfterServerRequestResolved.*)'

echo "[internal-app-guard] running full repository tests"
./scripts/with_tmp_go_cache.sh go test ./...

echo "[internal-app-guard] running focused internal/app guard suite"
./scripts/with_tmp_go_cache.sh go test -count=1 ./internal/app -run "$guard_regex"
