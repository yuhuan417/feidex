# Backend Layering

这份说明描述 `internal/app` 当前的 backend 分层约定。目标不是把 Codex 和 Claude 强行抹平成一套假抽象，而是把 Feishu 前端编排、backend 能力差异、以及具体协议实现放到明确且可验证的边界里。

## 1. Feishu Orchestration Layer

- 位于 root `internal/app`。
- 负责 Feishu 事件入口、session 路由、submission 队列、卡片动作、恢复流程、审批与 turn/thread 生命周期收口。
- 这层是产品行为层，不直接实现具体 backend 的 CLI/RPC 细节，但会协调 runtime 安装、pending request 路由、以及协议敏感恢复逻辑。
- 任何涉及 Codex app-server turn / thread / approval 生命周期的改动，都必须继续对照 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)。

代表文件：

- `internal/app/app.go`
- `internal/app/feishu_event_router.go`
- `internal/app/submission_queue.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/approval_actions.go`
- `internal/app/server_request_state.go`

## 2. Backend Driver / Capability Layer

- 主要位于 `internal/app/backend` 和 `internal/app/backendcaps`。
- 负责“当前 frontend 选中的 backend 能做什么、前端该如何展示什么、同一入口在不同 backend 下如何解释”。
- 允许在这一层按 backend 分流，但不在这里实现底层协议 transport。
- permission / workspace / conversation 术语差异都属于这一层的 owner；不应再散落到 root `app` 的多处 `switch configuredBackend(...)`。

当前入口：

- `internal/app/backend/driver.go`
  - backend driver、capability set、unsupported backend 入口。
- `internal/app/backend/permission_driver.go`
  - workspace / conversation permission 差异收口。
- `internal/app/backend/configuration.go`
  - model / status / workspace config 这类 backend-specific 配置入口分发。
- `internal/app/backend/selection.go`
  - backend 可用性检查与切换流程。
- `internal/app/backend/actions.go`
  - compact / interrupt 这类共享前端动作的 backend 分流。
- `internal/app/backendcaps/capability.go`
  - 会话/线程术语、帮助文案转换、菜单标签、命令可见性能力。

## 3. Conversation Implementation Layer

- 主要位于 `internal/app/convbackend`、`internal/app/clauderuntime`、`internal/app/claudesession`、`internal/app/codexruntime`，以及 `internal/codexrpc`。
- 负责真正的 Codex / Claude 行为实现。
- 只有这一层应该知道具体协议方法、CLI 特性、session/thread 启停细节、权限模式热更新、或 backend 内部恢复策略。

当前主要实现点：

- `internal/app/convbackend/service.go`
- `internal/app/conversation_backend_fork.go`
- `internal/app/conversation_backend_startup.go`
- `internal/app/clauderuntime/service.go`
- `internal/app/claudesession/*`
- `internal/app/codexruntime/*`
- `internal/codexrpc/*`

## 4. Runtime Installation / Protocol Bridge Layer

- 仍位于 root `internal/app`，因为这里需要直接读写 `*App` 里的 runtime 字段，并与前端生命周期状态机对齐。
- 这层不是新的业务 owner，而是把 backend runtime 和前端编排层接起来的最薄胶水。
- 它可以存在，但不能重新长成旧的 facade/shim 文件群。

当前入口：

- `internal/app/backend_runtime.go`
- `internal/app/backend_runtime_codex.go`
- `internal/app/backend_runtime_claude.go`
- `internal/app/backend_selection.go`
- `internal/app/backend_configuration_helpers.go`
- `internal/app/backend_server_request_adapter.go`

## 放置规则

- 新的前端展示差异、菜单差异、帮助文案差异，优先放到 `internal/app/backendcaps` 或 `internal/app/backend`。
- 新的 permission 差异，优先放到 `backend.PermissionDriver`，不要在 root `app` 做默认 backend 分支。
- 新的 backend 启停、runtime 维护、配置入口分发，优先放到 `internal/app/backend` 服务层；只有必须直接改 `*App` runtime 字段时，才留在 root glue。
- 新的 Codex / Claude 专有调用，留在 implementation layer，不要直接散落到 Feishu 编排入口。
- unset / unsupported backend 必须返回显式 unsupported 行为，不能静默 fallback 到 Codex 或任何默认 backend。
- root `internal/app` 不再接受新的 backend-specific compatibility shim、alias 文件、或 comment-only wrapper。
