# App Package Boundaries

`internal/app` is the Feishu-facing application coordinator. It owns product orchestration, session and turn lifecycle integration, pending request handling, card callbacks, and all behavior that must be checked against `docs/codex-app-server-state-machine-audit.md`. Code in this package may call Feishu adapters, backend runtime facades, the persistent store, and the extracted `internal/app/*` helper packages. It should not grow new pure parsing, formatting, or backend-agnostic workflow logic when that logic can live behind a smaller package boundary.

App carries 8 receiver methods (lifecycle: `Start`/`Stop`; event handlers: 5 `Handle*` methods; transport error coordination: `handleCodexTransportError`) and 209 package-level functions that take `*App` as their first parameter. Business logic is organized into ~30 small `{app *App}` service structs and package-level functions. There is no `commandService` or `conversationWorkflowService` — command routing and conversation workflows are package-level functions dispatched by the registries.

## Physical Subpackages

### `internal/app/delivery`

Responsibility: pure outbound delivery helpers for reply-card chunking and markdown splitting.

Allowed dependencies: Go standard library only.

Must not: depend on `internal/app`, call Feishu APIs, read or write application state, decide session/turn lifecycle, or perform network I/O.

Current owner surface:

- reply card payload/component limits shared by app delivery code
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

`internal/app` remains the composition root for behavior that crosses boundaries:

- Feishu event routing through the 5 `Handle*` receiver methods
- command dispatch via package-level `handleCommand` and local command registries (`localCommandSpec`)
- card action dispatch via package-level `dispatchCardAction` and action registries
- session, thread, turn, submission, queue, pending request, and recovery lifecycle
- Codex App Server state-machine integration documented by the audit
- backend runtime selection and facade calls
- review UI/form orchestration and review submission enqueueing
- download, workspace orchestration, model, approval completion, compaction, history, and notification product flows

Service structs group related behavior without introducing package boundaries:

- `workspaceService` / `workspaceConfigService` / `workspaceManagementService` / `workspaceRenderService` / `workspaceThreadService` — workspace lifecycle, config, rendering
- `threadService` — thread and session command/action handlers
- `backendConfigurationService` / `backendSelectionService` / `backendUpgradeService` — backend facade coordination
- `replyContinuationService` / `pendingInputService` / `pendingQueueService` — inbound message staging and queue
- `turnStreamService` / `outboundCardService` / `finalCardPatchService` — turn output streaming and card delivery
- `reviewFormService` / `reviewGitService` / `skillsService` — review and skills integration
- `runtimeStateService` / `runtimeMaintenanceService` / `autoRetryService` — runtime state and recovery
- `historyService` / `usageService` / `debugService` / `menuActionService` / `modelConfigService` — tools and diagnostics

When a new helper is pure or can be expressed behind a small interface, place it in a subpackage before adding more package-private functions to `internal/app`.

## Extraction Rules

- Subpackages must not import `internal/app`; use exported data structures or narrow interfaces instead.
- Lifecycle-sensitive code stays in `internal/app` until its protocol contract is explicit and covered by tests.
- Keep compatibility wrappers in `internal/app` only as migration shims; new code should import the owning subpackage directly when it does not need app coordination. Existing aliases for app-wide vocabulary should be removed once dependent call sites are migrated.
- Any change touching approvals, turn lifecycle, thread lifecycle, review submission, compaction, tool input, or server requests must be checked against the state-machine audit before merge.
- Prefer pure packages with standard-library dependencies. If a subpackage needs `config`, `state`, `codexrpc`, or `feishu`, document why that dependency belongs below the app coordinator.

## Planned Boundaries

These are not fully physical packages yet because current code still crosses app state, Feishu rendering, and lifecycle boundaries:

- `workspace`: move validation and filesystem planning helpers after config mutation boundaries are explicit.
- `runtime`: move capability probing and transition-state helpers after backend facade ownership is explicit.
- `approval`: move decision formatting only after backend reply/resume semantics are isolated behind tests.
- `cards`: move app-specific renderers only after markdown normalization and submission context dependencies are parameterized.

### Previously planned, now realized

- `quietmode` (was: "quiet mode evaluation") — extracted as `internal/app/quietmode` with pure delivery predicates.
- `sessionctx` (was: "session thread context") — extracted as `internal/app/sessionctx` with thread lifecycle and effective-value computation.
