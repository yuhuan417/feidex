# App Package Boundaries

`internal/app` is the Feishu-facing application coordinator. This document is the source of truth for where app-layer code belongs after the refactor. Historical phase plans are complete; new work should follow these steady-state boundaries.

## Root `internal/app`

Root `internal/app` keeps only code that must coordinate multiple owners or mutate frontend-scoped runtime state:

- Feishu event entrypoints and top-level routing
- command / menu / action registries and dispatch
- frontend-scoped session, submission, turn, and pending-request lifecycle
- Codex app-server protocol integration covered by [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)
- backend runtime installation / startup / failure coordination
- product flows that genuinely span multiple owners

Root should not grow new pure helpers, owner-local renderers, owner-local DTOs, or backend-specific workflow logic that can live behind a smaller package boundary.

## Main Extracted Owner Packages

This list is intentionally not exhaustive. It covers the main ownership boundaries that new work should reuse instead of expanding root `internal/app`.

### `internal/app/appstate`

Responsibility: frontend-scoped access to persistent app state and typed state-store helpers.

Allowed dependencies: `internal/state`, `internal/app/appcore`, `internal/app/sessionctx`, and Go standard library.

Must not: send Feishu messages, call backend RPC, own routing, or mutate runtime processes.

### `internal/app/backendcaps`

Responsibility: backend-specific capability flags and UI vocabulary shared by menu / help / status presentation.

Allowed dependencies: lightweight app value packages such as `runtime`, `menutypes`, and Go standard library.

Must not: mutate app/config/state, call backend RPC, or send Feishu messages.

### `internal/app/backend`

Responsibility: backend-aware frontend services that do not require importing root `internal/app`.

Allowed dependencies: lower `internal/app/*` packages plus `config`, `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, mutate `*App` fields directly, or bypass injected callbacks for root-owned lifecycle work.

### `internal/app/convbackend`

Responsibility: shared facade for concrete conversation operations and backend-specific thread / session workflows.

Allowed dependencies: backend-local helper packages plus `config`, `state`, `codexrpc`, and Go standard library.

Must not: own Feishu event routing, command / action registries, or pending-request persistence policy.

### `internal/app/serverrequest`

Responsibility: pending request creation, reply, cancel, elicitation, and user-input services that do not need to live in root.

Allowed dependencies: lower `internal/app/*` packages plus `feishu`, `state`, `codexrpc`, and Go standard library.

Must not: own Feishu entry routing, session / turn finalization, or backend runtime installation.

### `internal/app/submission`

Responsibility: submission queue orchestration, pending queue operations, staged-image helpers, and message-reaction bookkeeping that do not require importing root `internal/app`.

Allowed dependencies: lower `internal/app/*` packages plus `config`, `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, patch cards directly, or own backend runtime startup / shutdown.

### `internal/app/workspacecmd`

Responsibility: workspace configuration, creation, deletion, path-picker, and thread-binding services extracted from the root app package.

Allowed dependencies: lower `internal/app/*` packages plus `config`, `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, mutate `*App` fields directly, or own frontend-wide backend-switch policy.

### `internal/app/reviewcmd`

Responsibility: `/review` command orchestration, review form flows, and inline review start / enqueue helpers.

Allowed dependencies: `review`, lower `internal/app/*` packages plus `config`, `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, own generic submission-queue policy, or bypass `review` package target helpers.

### `internal/app/goalcmd`

Responsibility: `/goal` command behavior, goal cards/forms, thread-goal RPC calls, in-memory goal tracker, and active-goal continuation binding policy behind root lifecycle hooks.

Allowed dependencies: lower `internal/app/*` packages plus `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, bind Feishu root cards without the injected lifecycle callbacks, or weaken the state-machine audit rules for active goal continuations.

### `internal/app/mcpbridge`

Responsibility: the local Feidex MCP HTTP bridge, tool-call context resolution, Feishu local file/image/video send tool definitions, and Claude MCP config file generation.

Allowed dependencies: lower `internal/app/*` packages plus `state` and Go standard library.

Must not: import `internal/app`, read root runtime trackers directly, or decide frontend/backend publication policy outside injected dependencies.

### `internal/app/planmode`

Responsibility: Codex `/plan` collaboration-mode configuration, title decoration helpers, plan-exit confirmation cards, and post-plan implementation/fresh-thread flow behind root lifecycle hooks.

Allowed dependencies: lower `internal/app/*` packages plus `config`, `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, own generic command/action registries, or change plan item / turn lifecycle semantics without updating the state-machine audit.

### `internal/app/upgradecmd`

Responsibility: upgrade command orchestration and upgrade card flows, including local-binary upgrade picker / staging logic.

Allowed dependencies: lower `internal/app/*` packages plus `config`, `state`, `release`, `daemon`, `feishu`, and Go standard library.

Must not: import `internal/app`, mutate backend runtime fields directly, or own backend-specific smoke / restart behavior.

### `internal/app/delivery`

Responsibility: pure outbound delivery helpers for reply-card chunking and markdown splitting.

Allowed dependencies: Go standard library only.

Must not: depend on `internal/app`, call Feishu APIs, read or write application state, decide session / turn lifecycle, or perform network I/O.

### `internal/app/review`

Responsibility: review target data structures and Git-backed target discovery / resolution.

Allowed dependencies: Go standard library and the local `git` command through explicit command helpers.

Must not: depend on `internal/app`, render Feishu forms/cards, create pending requests, enqueue submissions, inspect sessions, or decide turn lifecycle behavior.

### `internal/app/maintenance`

Responsibility: backend-agnostic maintenance workflow primitives.

Allowed dependencies: Go standard library, or lower-level workflow packages when explicitly needed.

Must not: depend on `internal/app`, send Feishu cards, inspect active sessions directly, or own backend protocol behavior.

### `internal/app/runtime`

Responsibility: backend identity normalization and backend-agnostic runtime helper types.

Allowed dependencies: `internal/config` plus Go standard library.

Must not: start or stop backend processes, read or mutate sessions, perform recovery, send cards, or decide frontend switching eligibility.

### `internal/app/sessionctx`

Responsibility: session thread-context lifecycle and effective-value computation shared by app code.

Allowed dependencies: `internal/state`, `internal/config`, `internal/app/runtime`, and Go standard library.

Must not: depend on `internal/app`, call backend RPC, send Feishu messages, or start / stop threads.

### `internal/app/quietmode`

Responsibility: quiet-mode delivery predicates that determine which turn events should be delivered to the user.

Allowed dependencies: `internal/config` plus Go standard library.

Must not: depend on `internal/app`, read or mutate sessions, send Feishu messages, or inspect submissions.

### `internal/app/cards`

Responsibility: reusable Feishu card element construction that is independent of `App`.

Allowed dependencies: `internal/feishu` plus Go standard library.

Must not: call Feishu APIs, inspect submissions/sessions, normalize app-specific markdown, or decide card delivery/reuse.

## Root Retention Buckets

Every remaining root `package app` file should fit exactly one of these buckets:

- bootstrap / composition: `app.go`, `service.go`, `deps.go`, `accessors.go`, `app_deps.go`, `health.go`
- routing / entrypoints: `feishu_event_router.go`, `notifications.go`, `commands.go`, `command_registry.go`, `menu_*.go`, `action_registry*.go`, `feature_registry_bindings*.go`
- protocol-sensitive orchestration: `submission_queue.go`, `submission_workflow.go`, `submission_start_guard.go`, `turn_lifecycle.go`, `turn_stream.go`, `turn_binding.go`, `server_request_state.go`, `pending_inputs.go`, `claude_runtime.go`, `codex_*recovery.go`, `compact.go`
- minimal glue: `*_bindings.go`, `backend_selection.go`, `backend_runtime*.go`, `review_bindings.go`, `workspacecmd_bindings*.go`, `maintenance_bindings.go`, `goal_bindings.go`, `mcp_bindings.go`, `plan_mode_bindings.go`

Files that are only alias-only placeholders, comment-only migration stubs, or dead compatibility shims do not belong in root `app`.

## Extraction Rules

- Subpackages must not import `internal/app`; use exported data structures, narrow interfaces, or callback deps instead.
- Lifecycle-sensitive code stays in `internal/app` until its protocol contract is explicit and covered by tests.
- Root `internal/app` no longer accepts new compatibility shims. Do not add new root-level alias-only files, adapter-only files, placeholder wrappers, or comment-only migration stubs.
- New business logic should default to the owning subpackage first. Only keep logic in root `app` when it must coordinate Feishu entrypoints, `*App` field mutation, frontend-scoped lifecycle, or audited protocol transitions.
- Existing `*_bindings.go` files are tolerated only as minimal glue between subpackages and the root composition layer; they should not become dumping grounds for new product logic.
- Any change touching approvals, turn lifecycle, thread lifecycle, review submission, compaction, tool input, or server requests must be checked against the state-machine audit before merge.

## Current Extraction Candidates

These areas still have acceptable root glue, but they are the first places to keep slimming when nearby feature work happens:

- workspace / path-picker helpers
- review / history / debug / upgrade bindings
- backend runtime / maintenance / recovery glue
- oversized binding files such as `submission_bindings.go`, `serverrequest_bindings.go`, and `convbackend_bindings.go`
- goal / MCP / plan-mode bindings, if they start accumulating owner-local rendering or protocol-neutral helpers
