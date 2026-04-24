# App Package Boundaries

`internal/app` is the Feishu-facing application coordinator. It owns product orchestration, session and turn lifecycle integration, pending request handling, card callbacks, and all behavior that must be checked against `docs/codex-app-server-state-machine-audit.md`. Code in this package may call Feishu adapters, backend runtime facades, the persistent store, and the extracted `internal/app/*` helper packages. It should not grow new pure parsing, formatting, or backend-agnostic workflow logic when that logic can live behind a smaller package boundary.

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

## Root App Responsibilities

`internal/app` remains the composition root for behavior that crosses boundaries:

- Feishu event routing, command/action registries, and card callback acknowledgement
- session, thread, turn, submission, queue, pending request, and recovery lifecycle
- Codex App Server state-machine integration documented by the audit
- backend runtime selection and facade calls
- review UI/form orchestration and review submission enqueueing
- download, workspace, model, approval, compaction, history, and notification product flows

When a new helper is pure or can be expressed behind a small interface, place it in a subpackage before adding more package-private functions to `internal/app`.

## Extraction Rules

- Subpackages must not import `internal/app`; use exported data structures or narrow interfaces instead.
- Lifecycle-sensitive code stays in `internal/app` until its protocol contract is explicit and covered by tests.
- Keep compatibility wrappers in `internal/app` only as migration shims; new code should import the owning subpackage directly when it does not need app coordination.
- Any change touching approvals, turn lifecycle, thread lifecycle, review submission, compaction, tool input, or server requests must be checked against the state-machine audit before merge.
- Prefer pure packages with standard-library dependencies. If a subpackage needs `config`, `state`, `codexrpc`, or `feishu`, document why that dependency belongs below the app coordinator.

## Planned Boundaries

These are not fully physical packages yet because current code still crosses app state, Feishu rendering, and lifecycle boundaries:

- `workspace`: workspace config/create/clone primitives. App should keep message ownership, pending forms, and callback acknowledgement.
- `runtime`: backend normalization, capability probing, and runtime state helpers. App should keep session binding and recovery decisions.
- `approvals`: approval summary/decision formatting once the protocol-sensitive request lifecycle is isolated behind interfaces.
- `cards`: reusable card rendering primitives that do not call Feishu or mutate state.
