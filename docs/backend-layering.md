# Backend Layering

这份说明描述 `internal/app` 当前的 backend 分层约定。目标不是把 Codex 和 Claude 强行抹平成一套假抽象，而是把 Feishu 前端编排、backend 能力差异、以及具体协议实现放到明确且可验证的边界里。

## 1. Feishu Orchestration Layer

- 位于 root `internal/app`。
- 负责 Feishu 事件入口、session 路由、submission 队列、卡片动作、恢复流程、审批与 turn/thread 生命周期收口。
- 这层是产品行为层，不直接实现具体 backend 的 CLI / RPC 细节，但会协调 runtime 安装、pending request 路由、以及协议敏感恢复逻辑。
- 任何涉及 Codex app-server turn / thread / approval 生命周期的改动，都必须继续对照 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)。

代表文件:

- `internal/app/app.go`
- `internal/app/feishu_event_router.go`
- `internal/app/submission_queue.go`
- `internal/app/submission_workflow.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/server_request_state.go`

## 2. Backend Capability / Selection Layer

- 主要位于 `internal/app/backend` 和 `internal/app/backendcaps`。
- 负责“当前 frontend 选中的 backend 能做什么、前端该如何展示什么、同一入口在不同 backend 下如何解释”。
- 允许在这一层按 backend 分流，但不在这里实现底层协议 transport。
- permission / workspace / conversation 术语差异都属于这一层的 owner；不应再散落到 root `app` 的多处 backend 分支中。

当前入口:

- `internal/app/backend/driver.go`
- `internal/app/backend/permission_driver.go`
- `internal/app/backend/configuration.go`
- `internal/app/backend/selection.go`
- `internal/app/backend/actions.go`
- `internal/app/backend/display.go`
- `internal/app/backend/failure.go`
- `internal/app/backend/maintenance.go`
- `internal/app/backendcaps/capability.go`

## 3. Conversation Implementation Layer

- 主要位于 `internal/app/convbackend`、`internal/app/clauderuntime`、`internal/app/claudesession`、`internal/app/claudesupport`、`internal/app/codexruntime`，以及 `internal/codexrpc`。
- 负责真正的 Codex / Claude 行为实现。
- 只有这一层应该知道具体协议方法、CLI 特性、session/thread 启停细节、权限模式热更新、或 backend 内部恢复策略。

当前主要实现点:

- `internal/app/convbackend/service.go`
- `internal/app/convbackend/helpers.go`
- `internal/app/clauderuntime/service.go`
- `internal/app/clauderuntime/permission_updates.go`
- `internal/app/claudesession/catalog.go`
- `internal/app/claudesession/history.go`
- `internal/app/claudesupport/support.go`
- `internal/app/claudesupport/history.go`
- `internal/app/codexruntime/recovery.go`
- `internal/app/codexruntime/upgrade.go`
- `internal/codexrpc/*`

## 4. Runtime Bridge / Root Glue Layer

- 位于 root `internal/app`。
- 这层的职责是把 backend runtime 和前端编排层接起来，因为这里需要直接读写 `*App` 的 runtime 字段、frontend-scoped session 状态和恢复逻辑。
- 它不是新的业务 owner，只是薄胶水；不能重新长成旧的 facade / shim 文件群。

当前入口:

- `internal/app/backend_runtime.go`
- `internal/app/backend_runtime_codex.go`
- `internal/app/backend_runtime_claude.go`
- `internal/app/backend_selection.go`
- `internal/app/backend_configuration_helpers.go`
- `internal/app/maintenance_bindings.go`
- `internal/app/startup_recovery_bindings.go`
- `internal/app/clauderuntime_bindings.go`
- `internal/app/convbackend_bindings.go`

## 放置规则

- 新的前端展示差异、菜单差异、帮助文案差异，优先放到 `internal/app/backendcaps` 或 `internal/app/backend`。
- 新的 permission 差异，优先放到 `backend.PermissionDriver`，不要在 root `app` 做默认 backend 分支。
- 新的 backend 启停、runtime 维护、配置入口分发，优先放到 `internal/app/backend` 服务层；只有必须直接改 `*App` runtime 字段时，才留在 root glue。
- 新的 Codex / Claude 专有调用，留在 implementation layer，不要直接散落到 Feishu 编排入口。
- frontend 级 backend 切换和影响 session 启动语义的运行时配置变化，继续遵守 idle-only 规则。
- unset / unsupported backend 必须返回显式 unsupported 行为，不能静默 fallback 到 Codex 或任何默认 backend。
- root `internal/app` 不再接受新的 backend-specific compatibility shim、alias 文件、或 comment-only wrapper。
