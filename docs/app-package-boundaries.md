# App Package Boundaries

`internal/app` is the Feishu-facing application coordinator. It owns event entrypoints, product orchestration, session/thread/turn/submission lifecycle integration, pending request handling, card callbacks, and every behavior that must be checked against [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md). Code in this package may call Feishu transport, frontend-scoped app state, backend driver/runtime glue, and extracted `internal/app/*` subpackages. It should not grow new pure parsing, formatting, terminology, or backend-local workflow logic when that logic can live behind a smaller package boundary.

## Physical Subpackages

### `internal/app/backendcaps`

Responsibility: backend-specific capability flags and UI vocabulary shared by menu/help/status presentation.

Allowed dependencies: Go standard library, `internal/app/runtime`, and `internal/app/menutypes`.

Must not: mutate app/config/state, call backend RPC, or send Feishu messages.

Current owner surface:

- conversation slash/noun/id/help labels
- backend-visible feature flags
- help/menu terminology rewriting

### `internal/app/backend`

Responsibility: backend-aware frontend services that do not require importing root `internal/app`.

Allowed dependencies: lower `internal/app/*` packages plus `config`, `state`, `feishu`, `codexrpc`, and Go standard library.

Must not: import `internal/app`, mutate `*App` fields directly, or bypass injected callbacks for root-owned lifecycle work.

Current owner surface:

- backend driver model and unsupported-backend behavior
- permission drivers and backend-specific workspace/conversation setting flows
- backend selection/configuration/action services
- backend-visible status/help/menu support that depends on backend capabilities

### `internal/app/convbackend`

Responsibility: shared facade for concrete conversation operations and backend-specific thread/session workflows.

Allowed dependencies: backend-local helper packages plus `config`, `state`, `codexrpc`, and Go standard library.

Must not: own Feishu event routing, command/action registries, or pending request persistence policy.

Current owner surface:

- conversation start/interrupt/history/usage/compaction operations
- workspace thread/session binding helpers
- backend-specific fork/startup helpers exposed behind a common app-facing facade

### `internal/app/delivery`

Responsibility: pure outbound delivery helpers for reply-card chunking and markdown splitting.

Allowed dependencies: Go standard library only.

Must not: depend on `internal/app`, call Feishu APIs, read or write application state, decide session/turn lifecycle, or perform network I/O.

Current owner surface:

- reply-card payload/component limits shared by app delivery code
- markdown table and fence-aware splitting
- reply-card chunk data structures used before Feishu rendering/fitting

### `internal/app/maintenance`

Responsibility: backend-agnostic maintenance workflow primitives.

Allowed dependencies: Go standard library only unless a future workflow interface needs a lower-level package.

Must not: depend on `internal/app`, own daemon/release runtime state, send Feishu cards, inspect Codex/Claude sessions, or mutate pending requests directly.

Current owner surface:

- generic upgrade probe configuration
- reusable upgrade workflow decision logic
- no app-server protocol or Feishu callback behavior

### `internal/app/review`

Responsibility: review target data structures and Git-backed target discovery/resolution.

Allowed dependencies: Go standard library and the local `git` command through explicit command helpers.

Must not: depend on `internal/app`, render Feishu forms/cards, create pending requests, enqueue submissions, inspect sessions, or decide turn lifecycle behavior.

Current owner surface:

- review target constants and `TargetSpec`
- branch/commit option models and labels
- Git repository, branch, commit, diff, and working-tree probes used by review flows

### `internal/app/runtime`

Responsibility: backend identity normalization and backend-agnostic runtime helper types.

Allowed dependencies: `internal/config` plus Go standard library.

Must not: start or stop backend processes, read or mutate sessions, perform recovery, send cards, or decide frontend switching eligibility.

Current owner surface:

- canonical Codex/Claude backend constants
- backend normalization
- session in-flight mode primitives

### `internal/app/lifecycle`

Responsibility: pure lifecycle predicates that are shared by protocol-sensitive app code.

Allowed dependencies: `internal/state` plus Go standard library.

Must not: mutate pending requests, call backend RPC, finish turns, resume submissions, or send Feishu updates.

Current owner surface:

- server-resolved pending request kind classification
- pending request open/closed status predicate

### `internal/app/approval`

Responsibility: approval request presentation helpers.

Allowed dependencies: `internal/feishu`, `internal/pathdisplay`, and Go standard library.

Must not: complete approvals, reply to app-server requests, update pending request state, interrupt turns, or resume submissions.

Current owner surface:

- command approval body rendering
- file approval body rendering and file-entry extraction
- approval button construction

### `internal/app/workspace`

Responsibility: workspace workflow data models and workspace-local value objects.

Allowed dependencies: Go standard library only.

Must not: mutate config files, create pending forms, send cards, clone repositories, or decide active session binding.

Current owner surface:

- path picker payloads and entries
- workspace creation/clone payloads
- clone plan/progress/error value types

### `internal/app/quietmode`

Responsibility: quiet-mode delivery predicates that determine which turn events should be delivered to the user.

Allowed dependencies: `internal/config` plus Go standard library.

Must not: depend on `internal/app`, read or mutate sessions, send Feishu messages, or inspect submissions.

Current owner surface:

- `ShouldDeliverTurnKind`, `ShouldDeliverTurnItem`, `ShouldDeliverTurnItemPayload`
- `StatusText` for quiet-mode display
- `IsClaudeTodoToolPayload` classifier

### `internal/app/sessionctx`

Responsibility: session thread-context lifecycle and effective-value computation shared by app code.

Allowed dependencies: `internal/state`, `internal/config`, `internal/app/runtime` plus Go standard library.

Must not: depend on `internal/app`, call backend RPC, send Feishu messages, or start/stop threads.

Current owner surface:

- thread context lifecycle: `ClearThreadContext`, `SetThreadContext`, `SetThreadDefaults`
- effective values: `EffectiveApprovalPolicy`, `EffectiveSandboxMode`, `EffectiveClaudePermissionMode`
- workspace switching: `SwitchSessionWorkspace`, `CanResumeThreadForSubmission`
- backend thread snapshots: `BackendThreadSnapshot`, `StoreBackendThread`, `ClearBackendThread`, `RestoreBackendThread`

### `internal/app/cards`

Responsibility: reusable Feishu card element construction that is independent of `App`.

Allowed dependencies: `internal/feishu` plus Go standard library.

Must not: call Feishu APIs, inspect submissions/sessions, normalize app-specific markdown, or decide card delivery/reuse.

Current owner surface:

- markdown body card skeletons
- action rows
- static select elements

## Root App Responsibilities

`internal/app` remains the composition root only for behavior that crosses boundaries:

- Feishu event routing through the `Handle*` receiver methods
- command dispatch, menu dispatch, and card action dispatch
- session, conversation, turn, submission, queue, pending request, and recovery lifecycle
- Codex app-server protocol integration documented by the audit
- backend runtime installation/startup/transport-failure coordination
- server-request reply routing that depends on stored pending backend identity
- review UI/form orchestration and review submission enqueueing
- product flows such as workspace management, model/config menus, approval completion, compaction, history, usage, download, notifications, and maintenance entrypoints

Service structs and package-level helpers are acceptable in root `app` only when they coordinate multiple boundaries. If a new helper is pure or can be expressed behind a small interface, place it in a subpackage before adding more package-private logic here.

## Final Root Audit

Every remaining root `package app` file should fit exactly one of these buckets:

- bootstrap/composition: `app.go`, `deps.go`, `accessors.go`, `app_deps.go`
- routing/entrypoints: `feishu_event_router.go`, `commands.go`, `command_registry.go`, `menu_*.go`, `action_registry*.go`
- protocol-sensitive orchestration: `submission_queue*.go`, `turn_lifecycle.go`, `server_request_state.go`, `startup_recovery.go`, `approval_actions.go`, `pending_*.go`, `backend_runtime*.go`
- minimal glue: `*_bindings.go`, `backend_selection.go`, `backend_actions.go`, `backend_configuration_helpers.go`, `threadmenu_bindings.go`, `workspacecmd_bindings.go`

Files that are only alias-only placeholders, comment-only migration stubs, or dead compatibility shims do not belong in root `app`.

## Extraction Rules

- Subpackages must not import `internal/app`; use exported data structures, narrow interfaces, or callback deps instead.
- Lifecycle-sensitive code stays in `internal/app` until its protocol contract is explicit and covered by tests.
- Root `internal/app` no longer accepts new compatibility shims. Do not add new root-level alias-only files, adapter-only files, placeholder wrappers, or comment-only migration stubs.
- New business logic should default to the owning subpackage first. Only keep logic in root `app` when it must coordinate Feishu entrypoints, `*App` field mutation, frontend-scoped lifecycle, or audited protocol transitions.
- Existing `*_bindings.go` files are tolerated only as minimal glue between subpackages and the root composition layer; they should not become dumping grounds for new product logic.
- Any change touching approvals, turn lifecycle, thread lifecycle, review submission, compaction, tool input, or server requests must be checked against the state-machine audit before merge.
- Prefer pure packages with standard-library dependencies. If a subpackage needs `config`, `state`, `codexrpc`, or `feishu`, document why that dependency belongs below the app coordinator.

## Remaining Extraction Candidates

These areas still have acceptable root glue, but further extractions should follow the same boundary rules:

- `workspace`: move more validation and filesystem planning only after config mutation boundaries are explicit.
- `approval`: move additional presentation helpers only after backend reply/resume semantics stay covered by tests.
- `cards`: move more renderers only after submission context and markdown normalization inputs are parameterized.

### Already realized during this refactor

- `quietmode` — extracted as `internal/app/quietmode` with pure delivery predicates.
- `sessionctx` — extracted as `internal/app/sessionctx` with thread lifecycle and effective-value computation.
- `backendcaps` / `backend` / `convbackend` — extracted to own backend vocabulary, driver, and conversation implementation boundaries instead of root-level facade file families.
