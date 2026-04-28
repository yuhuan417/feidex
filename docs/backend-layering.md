# Backend Layering

这份说明描述 `internal/app` 当前的 backend 分层约定，目标是把 Feishu 前端编排、backend 能力选择、以及 Codex / Claude 具体实现分开。

## 1. Feishu Frontend / Orchestration Layer

- 负责 Feishu 事件入口、session 路由、submission 队列、卡片动作、恢复流程、审批与 turn/thread 生命周期收口。
- 这层是产品行为层，不应该直接实现具体 backend 的 CLI/RPC 细节。
- 任何涉及 Codex app-server turn / thread / approval 生命周期的改动，都必须继续对照 `docs/codex-app-server-state-machine-audit.md`。

代表文件：

- `internal/app/app.go`
- `internal/app/feishu_event_router.go`
- `internal/app/submission_queue.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/approval_actions.go`

## 2. Backend Capability Layer

- 负责“当前 frontend 选中的 backend 能做什么、前端该如何展示什么”。
- 允许在这一层按 backend 分流，但不在这里写具体的 Codex 协议调用或 Claude CLI 会话实现。

当前入口：

- `internal/app/backend_runtime_facade.go`
  - backend runtime 构建、启动、ready 检查、维护态和 transport failure 分发。
- `internal/app/backend_configuration_facade.go`
  - model / status / workspace config 这类 backend-specific 配置入口分发。
- `internal/app/backend_capability_facade.go`
  - 会话/线程术语、帮助文案转换、菜单标签、命令可见性能力。
- `internal/app/conversation_terms.go`
- `internal/app/menu_specs.go`
- `internal/app/menu_nav.go`

## 3. Backend Implementation Layer

- 负责真正的 Codex / Claude 行为实现。
- 只有这一层应该知道具体的协议方法、CLI 特性、权限模式热更新细节，或 backend 内部恢复策略。

当前主要实现点：

- `internal/app/conversation_backend_facade.go`
- `internal/app/conversation_backend_fork.go`
- `internal/app/conversation_backend_startup.go`
- `internal/app/claude_permission_config.go`
- `internal/app/claude_runtime.go`
- `internal/codexrpc/*`

## 放置规则

- 新的前端展示差异、菜单差异、帮助文案差异，优先放到 capability layer。
- 新的 backend 启停、runtime 维护、配置入口分发，优先放到 runtime / configuration facade。
- 新的 Codex / Claude 专有调用，留在 implementation layer，不要直接散落到 Feishu 编排入口。
- 如果两个 backend 都需要同一类抽象，先抽 capability / facade，再复用到底层实现。
- 在 [docs/internal-app-refactor-execution-plan.md](/home/yuhuan/feidex/docs/internal-app-refactor-execution-plan.md) 执行期间，不要继续在 root `internal/app` 里新增 backend-specific wrapper、alias、或 scattered `switch configuredBackend(...)` 逻辑；新差异必须优先落到 backend layer。
