# Backend Layering

本文记录当前 Codex/Claude backend 分层。目标不是抹平能力差异，而是让产品策略、协议转换、运行时监督和 Feishu 展示各有 owner。最终边界以 [长期架构目标与边界](architecture-refactor-proposal.md) 为准。

## 1. Application semantic layer

`internal/application` 负责 backend-neutral 的 conversation、submission、turn、interaction、workspace、routing、model configuration 和 capability policy。它只依赖 domain 与 consumer-owned ports，发布 typed `BackendEvent`、semantic effects 和 detached views；不得导入 `internal/codexrpc`、`internal/claudecli`、Feishu SDK、storage 或 `internal/app`。

主要入口：

- `internal/application/backendevents/service.go`
- `internal/application/backendops/request.go`
- `internal/application/conversation/service.go`
- `internal/application/submission/queue.go`
- `internal/application/turn/service.go`
- `internal/application/interaction/service.go`
- `internal/application/modelconfig/`
- `internal/application/routing/`

## 2. Feishu entry and composition layer

`internal/app` 只负责 Feishu callback binding；`internal/feishuapp` 承接现有 Feishu command/card 协议绑定，且只能由 `internal/composition` 创建。`internal/composition` 创建 frontend scope 与 runtime owner。卡片与消息由 `internal/adapter/feishu` 转换和执行，不能从 application 直接调用 SDK。

代表文件：

- `internal/feishuapp/input_dispatcher.go`
- `internal/feishuapp/backend_events.go`
- `internal/feishuapp/submission_bindings.go`
- `internal/feishuapp/turn_lifecycle.go`
- `internal/feishuapp/server_request_state.go`
- `internal/feishuapp/backend_runtime.go`

涉及 thread/turn/approval/review/compaction/tool input/server request 的改动，必须同步检查 [Codex 状态机审计](codex-app-server-state-machine-audit.md)。

## 3. Backend capability and configuration

backend 选择与 capability 位于 `internal/application/backendcaps`、`internal/application/backendconfig`、`internal/application/modelconfig` 和 `internal/feishuapp/backend_*` 的 composition glue。这里负责当前 frontend 的可用能力、配置 scope、模型 snapshot、idle-only backend switch 和失败策略；不实现具体 RPC 或 CLI wire。

`internal/feishuapp/backend_selection.go` 只连接 `internal/adapter/feishu/backend/selection.go` 的 semantic card/effect ports。backend failure、maintenance 和 runtime selection 的长任务必须遵守 frontend/session scope；不得静默 fallback 到 Codex。

## 4. Backend adapters

- `internal/adapter/backend/codex/` 负责 Codex gateway、thread/turn/review/goal/skills/input 编码、notification/request 解码、request token 和 usage 转换。
- `internal/adapter/backend/claude/` 负责 Claude conversation gateway 与 prompt/协议转换；Claude CLI session/process 的生命周期由 `internal/runtime/claude/` 监督。
- `internal/codexrpc/` 和 `internal/claudecli/` 只提供 transport/protocol 客户端与类型，不理解 Feishu、session aggregate 或卡片策略。

Codex wire 参数（如 thread start/resume/fork、turn start/steer 和 reply payload）必须留在 Codex adapter。架构测试会阻止 `internal/app` 重新组装这些类型；startup recovery 必须调用 Codex adapter operation。

## 5. Runtime layer

`internal/runtime` 负责 frontend lifecycle/cancellation、session actors、effect execution/dedupe、backend process supervision、recovery、定时任务执行、turn binding 和 workspace process。自动重试状态、退避策略、队列优先级、submission 创建和开关后的取消流程由 `internal/application/autoretry/` 负责，定时执行通过显式注入的 scheduler 完成。`FrontendOwner` 是 frontend-scoped mutable state 的单一构造入口；runtime 不导入 transitional app，也不承接菜单或产品策略。

当前主要实现：

- `internal/runtime/frontend.go`、`internal/runtime/frontend_owner.go`
- `internal/runtime/session_actors.go`
- `internal/runtime/effect_runner.go`、`internal/runtime/effect_deduper.go`
- `internal/runtime/codex/`、`internal/runtime/claude/`
- `internal/runtime/delayed_task.go`、`internal/runtime/turnbinding/`、`internal/runtime/workspace/`

## 放置规则

- 新产品规则放 application/domain；新协议字段放 backend adapter；新进程、取消、恢复和监督放 runtime；新卡片渲染/Feishu API 放 Feishu adapter；新构造只放 composition。
- session/submission/turn 的异步 continuation 必须通过 `RunSessionAsync` 或等价 actor port；frontend-wide startup/recovery/maintenance 重新按 session 投递队列工作。
- 所有 external effect 必须经过 semantic effect runner；保存失败不得发布后续 effects，成功副作用由 frontend-scoped deduper 记账。
- 新的 backend-specific compatibility shim、alias-only wrapper、service locator 或 `*App` capability carrier 不得加入 root。
