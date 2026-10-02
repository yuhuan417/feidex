# Backend Layering

这份说明记录从旧 `internal/app` backend 分层迁移到目标架构的边界。目标不是把 Codex 和 Claude 强行抹平成一套假抽象，而是把 Feishu 前端编排、backend 能力差异、以及具体协议实现放到明确且可验证的边界里。最终结构以 [长期架构重构提案](architecture-refactor-proposal.md) 为准。

## 1. Application Use Case Layer

- 最终位于 `internal/application`。
- 负责 frontend/session 路由、submission、turn、approval、model config 和 workspace 用例。
- 只依赖 domain 和 consumer-owned ports，不直接调用 Feishu SDK 或 Codex/Claude raw protocol。
- 迁移期间，`internal/app` 仍作为入口和兼容协调层调用这些用例。

## 2. Feishu Entry / Compatibility Layer

- 入口与组合位于 root `internal/app`，Feishu 协议和 outbound effect adapter 位于 `internal/adapter/feishu`。
- 负责 Feishu 事件入口、session 路由、submission 队列、卡片动作、恢复流程、审批与 turn/thread 生命周期收口。
- 这层只做入口和组合，不新增产品状态 owner；runtime 安装、pending request 路由和协议敏感恢复通过显式 capability 注入。
- 任何涉及 Codex app-server turn / thread / approval 生命周期的改动，都必须继续对照 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)。

代表文件:

- `internal/app/app.go`
- `internal/app/feishu_event_router.go`
- `internal/app/submission_queue.go`
- `internal/app/submission_workflow.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/server_request_state.go`

## 3. Backend Capability / Selection Layer

- 主要位于 `internal/app/backend` 和 `internal/application/backendcaps`。
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
- `internal/application/backendcaps/capability.go`

## 4. Backend Adapter Layer

- 最终位于 `internal/adapter/backend/codex` 和 `internal/adapter/backend/claude`，底层协议客户端仍位于 `internal/codexrpc` 与 `internal/claudecli`。协议实现已收敛到 `internal/adapter/backend` 与 `internal/runtime`；root 只保留 composition binding。
- 负责真正的 Codex / Claude 行为实现。
- 只有这一层应该知道具体协议方法、CLI 特性、session/thread 启停细节、权限模式热更新、或 backend 内部恢复策略。

当前主要实现点:

- `internal/adapter/backend/codex/*`
- `internal/adapter/backend/claude/*`
- `internal/runtime/claude/*`
- `internal/app/claudesupport/support.go`
- `internal/runtime/codex/recovery.go`
- `internal/runtime/codex/upgrade.go`
- `internal/codexrpc/*`

## 5. Runtime Layer

- 最终位于 `internal/runtime`。
- 这层负责 frontend 生命周期、进程监督、取消、恢复和 effect 执行；状态通过显式 runtime context 注入，避免直接回调 root。
- root glue 只负责构造和注入；runtime 不读取 root 宿主字段，也不承接产品规则。

当前入口:

- `internal/app/backend_runtime.go`
- `internal/app/backend_runtime_codex.go`
- `internal/app/backend_runtime_claude.go`
- `internal/app/backend_selection.go`
- `internal/app/backend_configuration_helpers.go`
- `internal/app/maintenance_bindings.go`
- `internal/app/startup_recovery_bindings.go`
- `internal/app/claude_runtime.go`
- `internal/app/conversation_services.go`

## 放置规则

- 新的前端展示差异、菜单差异、帮助文案差异，优先放到 `internal/application/backendcaps` 或 `internal/app/backend`。
- 新的 permission 差异，优先放到 `backend.PermissionDriver`，不要在 root `app` 做默认 backend 分支。
- 新的 backend 启停与维护放到 `internal/runtime`，协议操作放到 `internal/adapter/backend`，产品配置规则放到 application；root 只负责构造和注入。
- 新的 Codex / Claude 专有调用，留在 implementation layer，不要直接散落到 Feishu 编排入口。
- frontend 级 backend 切换和影响 session 启动语义的运行时配置变化，继续遵守 idle-only 规则。
- unset / unsupported backend 必须返回显式 unsupported 行为，不能静默 fallback 到 Codex 或任何默认 backend。
- root `internal/app` 不再接受新的 backend-specific compatibility shim、alias 文件、或 comment-only wrapper。
