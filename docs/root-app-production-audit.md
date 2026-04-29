# Root `internal/app` 生产文件现状

更新时间: 2026-04-29

这份文档记录 root `internal/app` 的当前快照，不再沿用 Phase 编号。规范边界以 [docs/app-package-boundaries.md](/home/yuhuan/feidex/docs/app-package-boundaries.md) 和 [docs/backend-layering.md](/home/yuhuan/feidex/docs/backend-layering.md) 为准；本文只回答“目前哪些生产文件还留在 root，为什么”。

## 快照

- root `internal/app` 当前有 `143` 个非 test Go 文件，共 `13,111` 行。
- `internal/app` 当前有 `59` 个直接子包。
- root 仍然非空是刻意设计：frontend-scoped lifecycle、Feishu 入口和 Codex app-server 协议敏感编排仍由 root 持有。

## 允许保留在 root 的 4 类文件

### 1. Bootstrap / Composition

代表文件:

- `app.go`
- `service.go`
- `deps.go`
- `accessors.go`
- `app_deps.go`
- `health.go`

保留原因:

- 创建 `*App`
- 注入依赖
- 暴露 frontend-scoped 服务与 accessor

### 2. Routing / Entry Points

代表文件:

- `feishu_event_router.go`
- `notifications.go`
- `commands.go`
- `command_registry.go`
- `menu_*.go`
- `action_registry*.go`
- `feature_registry_bindings*.go`

保留原因:

- 接收 Feishu 事件
- 维护 command / menu / action registry
- 把顶层入口路由到 owner package 或 root 编排逻辑

### 3. Protocol-Sensitive Orchestration

代表文件:

- `submission_queue.go`
- `submission_workflow.go`
- `submission_start_guard.go`
- `turn_lifecycle.go`
- `turn_stream.go`
- `turn_binding.go`
- `server_request_state.go`
- `pending_inputs.go`
- `claude_runtime.go`
- `codex_turn_recovery.go`
- `codex_runtime_recovery.go`
- `compact.go`

保留原因:

- 管理 frontend-scoped turn / submission / pending-request 状态
- 协调 Claude / Codex runtime 与本地 session 状态
- 这些路径必须持续对照 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)

### 4. Minimal Glue To Owning Packages

代表文件:

- `*_bindings.go`
- `backend_selection.go`
- `backend_runtime*.go`
- `review_bindings.go`
- `workspacecmd_bindings*.go`
- `maintenance_bindings.go`
- `finalcardpatch_bindings.go`
- `replycontinuation_bindings.go`

保留原因:

- 把 owner package 接到 root 组合根
- 保留 `*App` 级状态和入口协调
- 不重新拥有底层产品逻辑

## 当前仍偏重的 root 文件

截至 2026-04-29，行数最多的一批 root 生产文件是:

- `submission_bindings.go` — 313 行
- `app.go` — 310 行
- `serverrequest_bindings.go` — 257 行
- `claude_upgrade_runtime.go` — 251 行
- `convbackend_bindings.go` — 229 行
- `menu_actions.go` — 223 行
- `claude_support.go` — 194 行
- `feishu_event_router.go` — 193 行
- `accessors.go` — 193 行
- `feature_registry_bindings_thread_workspace.go` — 191 行
- `feature_registry_bindings.go` — 188 行
- `path_picker_actions.go` — 186 行
- `feature_registry_bindings_tools.go` — 186 行

这些文件目前仍可接受，但如果后续改动继续在这些文件里堆 owner-local helper 或渲染逻辑，就属于边界回退。

## 当前的继续收敛方向

后续优化按边界机会主义推进，不再维护单独的“next wave”计划:

- workspace / path-picker: owner-local validation、rendering、flow helper 继续压到 `workspacecmd`、`workspace`、`pathpick`
- upgrade / review / history / debug: 纯 command / form / render helper 优先压到 `upgradecmd`、`reviewcmd`、`historycmd`、`debugviewcmd`
- backend runtime / maintenance / recovery: owner-local backend 逻辑优先放到 `backend`、`convbackend`、`clauderuntime`、`codexruntime`、`maintenance`
- 大型 binding 文件: 如果 binding 文件开始吸收新的业务决策，而不是保持薄委托，就视为结构回退

## Guardrails

- 不新增新的 root alias-only 或 placeholder shim 文件。
- 不把纯格式化、纯 DTO、owner-local workflow logic 放回 root。
- 任何触及 approvals、turn/thread lifecycle、review、compaction、tool input、server requests 的改动，都必须对照状态机审计。
