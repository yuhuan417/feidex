# Developer Guide

This file is the working contract for human contributors and coding agents.
Prefer following this document over ad hoc local habits.

## Scope

Feidex is not a general chat bot. It is a bridge between Feishu message flows and a locally or remotely running agent backend. Today the production protocol contract is Codex App Server. Any non-Codex backend must be introduced behind a backend adapter without weakening the Codex path. Most changes should preserve three properties:

- Feishu-side interaction remains thread-aware and approval-safe.
- Codex-side protocol handling stays explicit and conservative.
- Build, release, and upgrade flows stay reproducible.

## Frontend Topology

Treat `frontend` as the runtime isolation boundary.

- One Feishu frontend maps to one active backend runtime at a time.
- A frontend backend may start unset; Feidex should force an explicit backend selection before queuing user work.
- One frontend must not multiplex between Codex and Claude concurrently at thread, session, or workspace scope.
- Backend switching is a frontend-level operation. It is allowed only when that frontend is fully idle: no active work, no queued/staged inputs, and no open pending approvals/forms.
- Any frontend-scoped runtime config change that affects backend session startup or resume semantics, such as Claude model or effort changes, must follow the same idle-only rule. Reject the change while a turn is active or pending work/forms exist; do not stage deferred resets to apply it later.
- Switching backend must preserve backend-scoped session lineage. If a user switches `codex -> claude -> codex`, the earlier Codex thread context for that frontend session should be restorable.
- If two frontends both use Codex, each frontend still owns its own Codex runtime process; do not share a single Codex app-server across multiple Feishu frontends.
- Workspace config is for repository path, sandbox, approval policy, model overrides, and similar worktree concerns. Backend selection must not be modeled as a workspace or thread switch.
- Shared persistent state must scope frontend-sensitive runtime keys, such as session keys, pending server requests, and message-link caches, so different frontends do not collide.

## Repository Layout

Use these boundaries when placing code:

| Path | Responsibility | Constraint |
| --- | --- | --- |
| `cmd/feidex` | CLI entrypoints and command parsing | Keep business logic out of `cmd/`; call into `internal/*`. |
| `cmd/feishu_card_demo` | Card rendering demo binary | Demo-only; do not couple production paths to it. |
| `internal/app` | Main product logic | Orchestrates sessions, submissions, menus, approvals, rendering, delivery, and protocol reactions. |
| `internal/feishu` | Feishu adapter layer | Owns SDK calls, outbound pacing, local file link rewrite, file sharing, and permission issue handling. Do not put app policy here. |
| `internal/codexrpc` | Codex App Server client and protocol types | Keep this transport/protocol-focused. No Feishu or app orchestration here. |
| `internal/config` | Config parsing, normalization, Feishu setup flows | Owns config file semantics and setup helpers. |
| `internal/state` | Persistent local state | Store and retrieval only; no UI logic. |
| `internal/daemon` | User-service install/run/upgrade | Keep daemon and systemd concerns isolated here. |
| `internal/release` | GitHub release metadata lookup/version selection | No CLI or Feishu logic here. |
| `internal/buildinfo` | Build-time version metadata | Only build/version helpers. |
| `claude-cli-protocol/` | Local reference material for Claude CLI stream-json protocol | Documentation/reference only. Do not wire `internal/app` directly to this protocol, and do not depend on the bundled Go SDK as a runtime integration layer; use this directory only to design Feidex-owned backend adapters. |
| `tmp/appserver-schema`, `tmp/appserver-ts` | Generated reference material | Reference only. Not runtime inputs and not release outputs. |

Dependency direction should stay simple:

- `cmd/*` may depend on `internal/*`.
- `internal/app` may depend on lower-level packages.
- Lower-level packages should not depend on `internal/app`.

## Interaction Constraints

- Any capability that is reachable from a Feishu menu must also be invocable directly from a slash command or equivalent command-line style entrypoint. Do not introduce menu-only product capabilities.
- Feishu-side user experience should stay as consistent as practical across different backends. If a backend-specific user-visible behavior must differ, confirm that difference with the user first and document the reason and constraint in the repository.
- Feishu card callback handlers must stay short and non-blocking. Do not perform long-running business logic, external network calls, or other high-latency work directly inside a synchronous card callback.
- If a card action needs slow work, acknowledge the callback quickly and continue via the asynchronous card update flow or another background path that can patch or replace the card later.

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
- Treat `/tmp` as a constrained resource on `tmpfs`-backed machines: keep temporary outputs and Feidex caches small, reuse standard cache directories instead of inventing ad hoc ones, and remove them promptly when the task is done.

Recommended commands:

```bash
mkdir -p bin
go build -o bin/feidex ./cmd/feidex
go build -o bin/feishu_card_demo ./cmd/feishu_card_demo
go test ./...
```

## Go Cache Convention

Use the system default Go cache when it is writable. If the environment is sandboxed or the default cache is read-only, use the Feidex-standard tmp locations instead of ad hoc names:

- `GOCACHE=/tmp/feidex-gocache`
- `GOMODCACHE=/tmp/feidex-gomodcache`

Do not invent task-specific cache directories such as random `go-build-*` or `probe-*` paths under `/tmp`.

Preferred entrypoint:

```bash
./scripts/with_tmp_go_cache.sh go test ./...
```

Cleanup:

```bash
./scripts/clean_tmp_go_cache.sh
```

Notes:

- The cleanup script restores owner write permission before deletion because Go module cache directories are commonly extracted as read-only.
- Use `FEIDEX_GOCACHE` and `FEIDEX_GOMODCACHE` only when you intentionally need non-standard locations.

### Local Integration Tests

Real Codex boundary tests are intentionally local-only:

Naming:

- Use `Codex Live Integration Tests` as the umbrella name for the real-Codex, `-tags=integration` test suite under `internal/codexrpc`.
- Use `Codex Live State-Machine Tests` for the live lifecycle/state-machine subset, mainly `integration_live_state_machine_test.go`.
- Do not use those names for fake-client contract tests under `internal/app`, such as `state_machine_contracts_test.go`.

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

Run them as separate commands. Do not fold them into the default `go test ./...` verification step.

Codex smoke boundary:

```bash
export FEIDEX_CODEX_COMMAND=codex
export FEIDEX_CODEX_CWD=/absolute/path/to/a/worktree
./scripts/with_tmp_go_cache.sh go test -tags=integration ./internal/codexrpc -run TestLiveCodexInitializeModelListAndThreadRead
```

The smoke test above is intentionally cheap because it avoids `turn/start`, `turn/steer`, and `review/start`.

Expensive Codex review lifecycle over stdio:

```bash
export FEIDEX_CODEX_COMMAND=codex
export FEIDEX_CODEX_RUN_TOKEN_TESTS=1
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexInlineReviewLifecycleOnTinyRepo
```

Token-consuming Codex state-machine probes over stdio:

```bash
export FEIDEX_CODEX_COMMAND=codex
export FEIDEX_CODEX_RUN_TOKEN_TESTS=1
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexTurnLifecycleCoreOnTinyRepo
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexSteerContinuationOnActiveTurn
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexCommandApprovalLifecycleOnTinyRepo
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexNeverApprovalPolicyRunsCommandWithoutServerRequest
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexFileApprovalLifecycleOnTinyRepo
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexInlineReviewLifecycleOnTinyRepo
```

Notes:

- The review lifecycle test creates its own tiny temporary git repository inside the test. It intentionally does not review the current Feidex worktree.
- The turn lifecycle, steer, command approval, and file approval live tests also create their own tiny temporary repositories. Do not repoint them at this repository.
- `TestLiveCodexNeverApprovalPolicyRunsCommandWithoutServerRequest` is the cheap reverse-guard for approval semantics: it uses the same tiny random-prefixed local script fixture, but with `approvalPolicy=never`, and fails if Codex still emits any approval request.
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

### Multi-Backend Boundary

If Feidex adds another backend, keep these rules:

- `docs/codex-app-server-state-machine-audit.md` remains the authoritative protocol contract for the Codex backend path.
- A Claude backend is a separate backend, not a partial implementation of the Codex app-server protocol.
- `internal/app` should target backend-neutral product semantics such as conversation binding, turn started/completed, output deltas, tool lifecycle, approvals, user-input requests, and usage updates.
- Raw protocol method names, envelopes, and transport semantics belong in backend-specific adapters, not in `internal/app`.
- `claude-cli-protocol/` is reference material only. Do not build Feidex's Claude backend by depending on its bundled Go SDK; Feidex should own its Claude stream-json adapter and parsers locally.
- Do not collapse the product to the lowest common denominator. Shared abstractions should cover the common lifecycle, while backend-specific capabilities remain first-class product features.
- Do not make one backend the semantic parent of the other. Backend-neutral types are for shared orchestration only; backend-native capabilities should live behind explicit extension points.
- Capability differences must be explicit. If a backend does not support review, compaction, thread history, skills catalog, or model catalog parity, gate the product surface instead of allowing protocol errors to leak through.
- Use [docs/claude-cli-backend-design.md](/home/yuhuan/feidex/docs/claude-cli-backend-design.md) as the current design reference for the Claude dual-backend direction.

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

### Feishu Card Callback Contract

Feishu card actions are a latency-critical ack path. Treat them as short control-plane handlers, not as a place to run full workflows.

Rules:

- Card callbacks must return quickly. Assume the timeout budget is strict and user-visible.
- Do not perform blocking network I/O directly inside a card callback.
- Do not run long local processes directly inside a card callback, including `git clone`, `git fetch`, archive extraction, large directory scans, or similar work.
- Do not start long Codex or other multi-step workflows directly inside a card callback when the first user-visible response can be returned earlier.
- Card callbacks may validate input, update pending state, enqueue work, and return a toast or replacement card.
- Heavy work must move to an async path: enqueue background work first, acknowledge the callback immediately, then patch the card or send a follow-up message when the background step finishes.
- If a new card action can sometimes be fast but can also block on network, disk, subprocesses, or external services, treat it as heavy and keep it out of the callback path.

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

- Missing or empty quiet value normalizes to `progress`.
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

Additional rule for Feishu card actions:

- If the action can trigger clone, download, upload, fetch, review, upgrade, or any other potentially slow workflow, split it into `fast callback ack` plus `async execution`, rather than doing the work inline in the callback handler.

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
