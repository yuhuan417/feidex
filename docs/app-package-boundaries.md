# App Package Boundaries

本文记录当前 `internal/app` 的稳定边界。目标 ownership 和依赖方向以 [architecture-refactor-proposal.md](architecture-refactor-proposal.md) 与 [DEVELOPER.md](../DEVELOPER.md) 为准；新代码不得通过新增 shim、service locator 或宽 `App` 接口扩大 root。

## Thin root `internal/app`

root app 只保留 Feishu callback binding：

- `entrypoint.go`：只定义 Feishu transport 的窄 handler 接口并安装四类 Feishu callback。
- `doc.go`：声明边界和依赖方向。

应用入口实现位于 `internal/feishuapp`，由 `internal/composition` 直接构造；它不属于 `internal/app` 的兼容转发层。`internal/app` 不持有业务状态、服务注册表、backend client、业务 tracker 或跨 owner callback。

`internal/feishuapp` 是 Feishu frontend 的实现包，承接现有 Feishu command/card 协议绑定；其生产实例只能由 `internal/composition` 创建。新的 domain/application/runtime 能力不得继续增加到该包，应落到对应 owner。

## Application owners

| Package | Owner | Boundary |
| --- | --- | --- |
| `internal/application/conversation` | conversation/session transitions | 创建、恢复、选择、fork 和 lineage；只消费 ports |
| `internal/application/submission` | submission queue/start/finalize | queued/running/waiting/terminal 与 queue continuation；不发送 SDK 卡片 |
| `internal/application/turn` | turn lifecycle | started/completed/failed/interrupted、steer 和下一条队列；不持有 `*App` |
| `internal/application/interaction` | pending interaction | pending/replied/resolved 状态和 authoritative resolve |
| `internal/application/asyncinput` | async user input | claim、答案路由和失败恢复 |
| `internal/application/workspace` | workspace lifecycle | selection、创建、删除、绑定和持久化前置检查 |
| `internal/application/routing` | frontend/chat routing | primary、AgentBinding、BotProfile 和 scope 校验 |
| `internal/application/modelconfig` | model desired/applied/snapshot | 配置 revision 与 turn 启动快照 |
| `internal/application/backendevents` | backend event sink | typed backend event 到 owner 的入口 |
| `internal/application/presentation` | detached views | 只产出 renderer 可消费的语义值 |

Application 不依赖 `internal/app`、`internal/adapter`、`internal/runtime`、`internal/state`、SDK 或 raw backend protocol；这些约束由 `internal/architecture` 测试守卫。

## Adapters

- `internal/adapter/backend/codex` 与 `internal/adapter/backend/claude` 负责协议字段、method、CLI prompt/stream 和 backend capability 转换；`internal/codexrpc`、`internal/claudecli` 只保留 transport/protocol。
- `internal/adapter/feishu` 负责 Feishu event conversion、card renderer、pending form、delivery、outbound effect 执行、文件与权限适配。adapter 不决定 turn/session 产品策略。
- `internal/adapter/storage/json` 与 `internal/state` 负责 DTO、scope、clone、锁和保存；repository 不执行网络、卡片、RPC 或进程。

Feishu adapter 的命令和菜单必须同时提供直接 command 入口；慢 callback 遵循 `fast ack -> async work -> patch/follow-up`。`card.action.trigger` 的声明与 handler 由 `internal/feishu/events.go` 同一注册表维护。

## Runtime and composition

`internal/composition` 构造 frontend scope、state gateway、adapter、`internal/feishuapp` frontend 和 runtime owner。`internal/runtime/frontend_owner.go` 持有 `SessionActors`、`FrontendRuntime`、live threads、retry/recovery、client registry 和 effect deduper；不同 frontend 不共享 mutable runtime。`internal/runtime/turnbinding` 只持有 turn binding，`internal/runtime/workspace` 只持有进程型 workspace 操作。

新增能力提交前应能回答：它修改哪个 domain aggregate、由哪个 application owner 处理、需要哪些 ports、产生哪些 effects、由哪个 adapter 执行、是否影响 Codex 状态机，以及 frontend/chat/session scope 是什么。
