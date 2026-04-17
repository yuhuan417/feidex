# Developer Guide

This file is the working contract for human contributors and coding agents.
Prefer following this document over ad hoc local habits.

## Scope

Feidex is not a general chat bot. It is a bridge between Feishu message flows and a locally or remotely running Codex App Server. Most changes should preserve three properties:

- Feishu-side interaction remains thread-aware and approval-safe.
- Codex-side protocol handling stays explicit and conservative.
- Build, release, and upgrade flows stay reproducible.

## Repository Layout

Use these boundaries when placing code:

| Path | Responsibility | Constraint |
| --- | --- | --- |
| `cmd/feidex` | CLI entrypoints and command parsing | Keep business logic out of `cmd/`; call into `internal/*`. |
| `cmd/feishu_card_demo` | Card rendering demo binary | Demo-only; do not couple production paths to it. |
| `internal/app` | Main product logic | Orchestrates sessions, submissions, menus, approvals, rendering, delivery, and protocol reactions. |
| `internal/feishu` | Feishu adapter layer | Owns SDK calls, outbound pacing, markdown preview, file sharing, and permission issue handling. Do not put app policy here. |
| `internal/codexrpc` | Codex App Server client and protocol types | Keep this transport/protocol-focused. No Feishu or app orchestration here. |
| `internal/config` | Config parsing, normalization, Feishu setup flows | Owns config file semantics and setup helpers. |
| `internal/state` | Persistent local state | Store and retrieval only; no UI logic. |
| `internal/daemon` | User-service install/run/upgrade | Keep daemon and systemd concerns isolated here. |
| `internal/release` | GitHub release metadata lookup/version selection | No CLI or Feishu logic here. |
| `internal/buildinfo` | Build-time version metadata | Only build/version helpers. |
| `tmp/appserver-schema`, `tmp/appserver-ts` | Generated reference material | Reference only. Not runtime inputs and not release outputs. |

Dependency direction should stay simple:

- `cmd/*` may depend on `internal/*`.
- `internal/app` may depend on lower-level packages.
- Lower-level packages should not depend on `internal/app`.

## Build Output Rules

Do not scatter binaries around the repository. Use these locations consistently:

- `bin/`: local development binaries built on the current machine.
- `dist/`: release-style artifacts and packaged outputs.
- `tmp/`: generated schemas, experiments, scratch files, and one-off temporary outputs.

Rules:

- Do not place compiled binaries in the repo root.
- Do not use `dist/` for casual local test builds.
- Do not use `tmp/` for release artifacts.
- `bin/` and `dist/` contents should remain disposable and normally untracked.

Recommended commands:

```bash
mkdir -p bin
go build -o bin/feidex ./cmd/feidex
go build -o bin/feishu_card_demo ./cmd/feishu_card_demo
go test ./...
```

### Local Integration Tests

Real Codex boundary tests are intentionally local-only:

- They may require live credentials or a running local app-server endpoint.
- Any live test that starts a real Codex turn or review consumes real tokens.
- Review lifecycle tests are usually the most expensive token consumers, but they are not the only ones.
- They must not run in GitHub CI.
- They must not run as part of `go test ./...`, pre-push hooks, or routine local verification.
- They are compiled only when you pass `-tags=integration`.
- They must be triggered manually, one named test per command.
- Any token-consuming live test must stay off by default and require `FEIDEX_CODEX_RUN_TOKEN_TESTS=1`.
- Review tests must use tiny prepared fixtures to control token burn. Do not point them at this repository.

Required environment variables:

- Codex stdio path:
  - `FEIDEX_CODEX_COMMAND` (optional, defaults to `codex`)
  - `FEIDEX_CODEX_CWD` (optional, defaults to the package working directory)
- Codex WebSocket mode:
  - `FEIDEX_CODEX_TRANSPORT=ws`
  - `FEIDEX_CODEX_WS_URL`
  - `FEIDEX_CODEX_WS_BEARER_TOKEN` when the endpoint requires bearer auth

Run them as separate commands. Do not fold them into the default `go test ./...` verification step.

Codex smoke boundary:

```bash
export FEIDEX_CODEX_COMMAND=codex
export FEIDEX_CODEX_CWD=/absolute/path/to/a/worktree
go test -tags=integration ./internal/codexrpc -run TestLiveCodexInitializeModelListAndThreadRead
```

The smoke test above is intentionally cheap because it avoids `turn/start`, `turn/steer`, and `review/start`.

Codex smoke boundary over WebSocket:

```bash
export FEIDEX_CODEX_TRANSPORT=ws
export FEIDEX_CODEX_WS_URL=wss://example.com/codex
export FEIDEX_CODEX_WS_BEARER_TOKEN=token
export FEIDEX_CODEX_CWD=/absolute/path/to/a/worktree
go test -tags=integration ./internal/codexrpc -run TestLiveCodexInitializeModelListAndThreadRead
```

Expensive Codex review lifecycle over stdio:

```bash
export FEIDEX_CODEX_COMMAND=codex
export FEIDEX_CODEX_RUN_TOKEN_TESTS=1
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexInlineReviewLifecycleOnTinyRepo
```

Expensive Codex review lifecycle over WebSocket:

```bash
export FEIDEX_CODEX_TRANSPORT=ws
export FEIDEX_CODEX_WS_URL=wss://example.com/codex
export FEIDEX_CODEX_WS_BEARER_TOKEN=token
export FEIDEX_CODEX_RUN_TOKEN_TESTS=1
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexInlineReviewLifecycleOnTinyRepo
```

Token-consuming Codex state-machine probes over stdio:

```bash
export FEIDEX_CODEX_COMMAND=codex
export FEIDEX_CODEX_RUN_TOKEN_TESTS=1
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexTurnLifecycleCoreOnTinyRepo
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexSteerContinuationOnActiveTurn
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexCommandApprovalLifecycleOnTinyRepo
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexFileApprovalLifecycleOnTinyRepo
go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexInlineReviewLifecycleOnTinyRepo
```

Notes:

- The review lifecycle test creates its own tiny temporary git repository inside the test. It intentionally does not review the current Feidex worktree.
- The turn lifecycle, steer, command approval, and file approval live tests also create their own tiny temporary repositories. Do not repoint them at this repository.
- Any future live test that calls `turn/start`, `turn/steer`, or `review/start` must follow the same manual-only rule and stay behind `FEIDEX_CODEX_RUN_TOKEN_TESTS=1`.
- Keep the smoke test and the expensive review test as separate commands so token spend stays explicit and predictable.
- As of 2026-04-17, `TestLiveCodexCommandApprovalLifecycleOnTinyRepo` uses a tiny random-prefixed local script fixture instead of a common shell command. Repeated local runs against real Codex with `approvalPolicy=on-request` triggered `item/commandExecution/requestApproval` reliably for that fixture.
- Earlier probes that used common shell wrappers such as `/bin/bash -lc ...` produced false negatives, including direct execution without a protocol-level approval request. Do not switch this test back to a common trusted-looking command shape.

Release-style local builds, if needed, should mimic CI naming and go to `dist/`:

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/feidex-linux-amd64 ./cmd/feidex
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o dist/feidex-linux-aarch64 ./cmd/feidex
```

## Build And Release Requirements

Local development:

- Default binary path: `bin/feidex`
- Default config path in examples: `config.toml`
- Standard verification before commit: `go test ./...`

Release flow:

- Use `./scripts/create_github_release.sh [version]` to create release tags.
- Run release tagging only from a clean `main` that matches `origin/main`.
- Do not hand-roll release tags when the script already covers the case.

GitHub Actions behavior:

- Push to `main`: runs CI and publishes `dev-latest` Linux assets.
- Push tag `v*`: runs tests and publishes stable Linux release assets.
- CI uses `dist/` for release assets. Keep local conventions aligned with that.

Current release asset names are:

- `feidex-linux-amd64`
- `feidex-linux-aarch64`
- matching `.tar.gz` archives
- `sha256sums.txt`

If those names change, update the workflow, release lookup logic, and daemon upgrade paths together.

## Deployment Notes

Normal run:

```bash
bin/feidex serve --config config.toml
```

Daemon management:

```bash
bin/feidex daemon install --config config.toml
bin/feidex daemon status
bin/feidex daemon restart
```

Notes:

- `daemon upgrade-runner` is an internal operational path; do not document it as the main user flow.
- Linux user-systemd behavior is owned by `internal/daemon`.

## Protocol Notes

### Codex App Server

Feidex currently treats completed turn items as the primary source of truth for user-visible output.

Notifications explicitly handled today:

- `item/completed`
- `turn/plan/updated`
- `turn/started`
- `turn/completed`
- `thread/tokenUsage/updated`
- `error`
- `serverRequest/resolved`

Server requests explicitly handled today:

- `item/commandExecution/requestApproval`
- `item/fileChange/requestApproval`
- `item/permissions/requestApproval`
- `item/tool/requestUserInput`
- `mcpServer/elicitation/request`

Guidance:

- Do not add product logic that depends on item deltas or snapshots unless the implementation is intentionally expanded to consume them.
- When introducing a new item type, update normalization, rendering, quiet-mode handling, and tests together.

The usual files to touch for new item types are:

- `internal/app/turn_item_payload.go`
- `internal/app/turn_item_cards.go`
- `internal/app/quiet_working_card.go`
- relevant tests under `internal/app/*test.go`

### Feishu

Important current assumptions:

- Session identity differs between p2p and group reply trees.
- Outbound message creation and patching are paced in `internal/feishu/message_rate_limit.go`.
- New message creation is globally paced.
- Card patching is paced per message ID.

Current pacing constants are tied to `im/v1/messages` behavior:

- create: `5 QPS`
- patch: `5 QPS`

If you add new outbound paths, prefer reusing adapter helpers instead of calling the raw SDK directly, otherwise pacing and logging will be bypassed.

### Path Rendering

User-visible file paths should follow one rule consistently:

- If the path is inside the active workspace, render it relative to the workspace root.
- If it is outside the workspace, render the full path.

Do not mix basename-only rendering in one surface and full-path rendering in another unless there is a deliberate UX reason.

## Quiet Mode Contract

Canonical quiet modes are:

- `verbose`
- `progress`
- `normal`
- `final`

Semantics:

- `verbose`: fully expanded process messages.
- `progress`: condensed process updates via the working card.
- `normal`: only agent messages, plans, and final output.
- `final`: final answer only.

Config behavior:

- Missing or empty quiet value normalizes to `verbose`.
- Invalid configured quiet values fall back to `normal` instead of aborting startup.

Implementation rule:

- Only `progress` may create or update the dynamic `工作中` card.
- If a working card already exists and the mode changes, turn-boundary and turn-finish code must still drain or reuse it instead of leaving it orphaned.

## Change Checklists

### When Adding Or Changing A CLI Command

Update all relevant surfaces:

- `cmd/feidex/*` for top-level CLI parsing
- `internal/app/command_registry.go` for in-chat slash command discovery/help
- menu/action routing if the command is exposed in cards
- tests

### When Adding Or Changing A Menu Action

Keep these in sync:

- `internal/app/menu_specs.go` / `menu_registry.go` / `menu_nav.go`
- `internal/app/action_registry.go`
- `internal/app/menu_actions.go`
- tests for dispatch and card rendering

### When Adding Or Changing Config

Sync all of:

- `internal/config/*`
- `config.example.toml`
- `README.md`
- status/menu surfaces if users can inspect or change the value
- tests

### When Adding New User-Facing Output

Check:

- quiet-mode delivery gating
- reply card splitting behavior
- message link tracking
- workspace-relative path rendering
- tests for both card and fallback text behavior

## Documentation Hygiene

If behavior changes materially, update:

- `README.md` for user-facing setup/run docs
- `DEVELOPER.md` for contributor constraints and conventions
- `config.example.toml` for current config shape

Do not let examples drift from the codebase.
