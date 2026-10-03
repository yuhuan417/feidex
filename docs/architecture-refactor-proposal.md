# 长期架构目标与边界

状态：目标架构定义
更新时间：2026-10-03

本文只定义 Feidex 的最终架构目标、职责边界和完成判定，不记录阶段拆解、执行顺序或验收过程。实现必须继续遵守 [DEVELOPER.md](../DEVELOPER.md) 的工程契约和 [Codex App Server 状态机审计](codex-app-server-state-machine-audit.md) 的协议约束；thread、turn、approval、server request 和 goal continuation 的行为不能因为架构调整而改变。

## 1. 最终目标与边界

最终目标是让每个领域状态只有一个 owner，让跨模块控制流只通过显式的 input、command、event、query 和 effect 传递。目录移动不是完成标准，依赖方向和运行时所有权才是完成标准。

最终运行时拓扑如下：

- `internal/composition` 是唯一 composition root，负责创建 frontend scope、repository、application use case、adapter、runtime owner 并注入依赖。
- `internal/app` 只保留 Feishu 入口和极薄的输入转换/分发门面。它不持有业务状态、服务注册表、backend client、业务 tracker 或跨 owner callback，也不负责组装 backend wire 请求。
- `internal/feishuapp` 承接现有 Feishu frontend 的协议绑定实现，但只能由 `internal/composition` 构造；它不是独立 composition root，也不向外提供兼容转发层。
- `internal/domain` 负责聚合、值对象和不变量；`internal/application` 负责产品用例、策略、查询和 semantic effects；`internal/adapter` 负责 Feishu、Codex、Claude、配置和存储协议转换；`internal/runtime` 负责 frontend 生命周期、进程、取消、恢复、并发和 effect 执行。
- `internal/state` 和其他 repository 只负责持久化、clone、normalize 和原子更新，不发送消息、不渲染卡片、不调用 backend 或启动进程。
- 每个 frontend 拥有独立的 backend runtime、session actors、pending requests、message links 和 runtime cache；同一 session 的状态转换串行，不同 session 和 frontend 之间互不共享 mutable runtime。

最终边界必须消除以下控制流：

```text
App
  -> Service
  -> callback / App interface
  -> App
  -> another Service
```

```text
Feishu / Codex / Claude / Store
          │
       adapters
          │ typed input/event/query result
          ▼
application use case ── semantic effects ──▶ runtime / adapter
          │
          ▼
   domain aggregate + repository ports
```

## 2. 目标依赖方向

```text
                         ┌─────────────────────┐
                         │ Feishu / Codex /     │
                         │ Claude / JSON Store  │
                         └──────────┬──────────┘
                                    │ adapters
                                    ▼
┌──────────────┐       ┌─────────────────────┐       ┌──────────────┐
│ Input events │ ────▶ │ Application usecase │ ────▶ │ Effects      │
└──────────────┘       └──────────┬──────────┘       └──────┬───────┘
                                   │                         │
                                   ▼                         ▼
                         ┌─────────────────────┐   ┌────────────────┐
                         │ Domain reducers     │   │ Effect runner  │
                         │ and invariants      │   │ and runtime    │
                         └──────────┬──────────┘   └────────────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │ Repository ports    │
                         └─────────────────────┘
```

依赖规则：

- domain 不依赖 Feishu、Codex、Claude、config、storage 或 `App`。
- application 只依赖 domain 和由使用方定义的 ports，不直接调用 SDK 或具体协议。
- adapter 负责把外部协议转换成 application input/event，把 application effect 转换成外部调用。
- runtime 负责进程、worker、取消、重启、并发和 effect 执行，不决定产品规则。
- composition root 只负责构造对象、注入依赖和启动系统。

接口应由使用方定义在对应 application 模块中，避免重新创建一个全局的 `appcore` 或巨大的 `ports` 包。

## 3. 最终目录结构

```text
internal/
  domain/
    identity/       # frontend、chat、session、thread、message 等值对象
    routing/        # primary、BotProfile、AgentBinding、路由规则
    conversation/   # session、backend thread lineage、workspace 关联
    submission/     # 输入、排队、运行、完成、失败
    turn/           # turn 绑定和生命周期状态
    interaction/    # approval、user input、elicitation
    modelconfig/    # desired、applied、turn snapshot
    workspace/      # workspace 领域值对象和约束

  application/
    input.go        # MessageReceived、CardActionReceived、BackendEvent 等
    effect.go       # SaveState、SendMessage、StartTurn、PatchCard 等
    dispatcher.go   # 输入分发到 use case
    presentation/   # 与具体 Feishu SDK 无关的语义化 view model
    routing/
    conversation/
    submission/
    turn/
    approval/
    modelconfig/
    workspace/
    menu/

  adapter/
    feishu/
      inbound.go
      card_actions.go
      cards.go
      messages.go
    backend/
      codex/
        protocol.go
        adapter.go
        events.go
      claude/
        protocol.go
        adapter.go
        events.go
    storage/
      json/

  runtime/
    frontend.go
    session_actor.go
    backend_supervisor.go
    effect_runner.go
    startup_recovery.go
    shutdown.go

  composition/
    app.go
```

目录结构表达最终 owner；实现可以暂时保留兼容目录，但不得改变这里定义的依赖方向和状态所有权。

## 4. 领域状态和 owner

### FrontendRuntime

负责一个 frontend 的生命周期和隔离边界：

- 一个 frontend 对应一个 active backend runtime。
- backend、session lineage、pending request、message link 和 runtime cache 都按 frontend 隔离。
- backend 切换、升级、重启和 shutdown 属于 frontend runtime，不属于 workspace 或 thread。

### Conversation

负责 Feishu session 与 backend conversation/thread 的关系：

- 当前 workspace。
- 当前 backend thread/session。
- 按 backend 保存的 thread lineage。
- 是否可以恢复、fork 或切换 workspace。

### Submission

负责一次用户输入从创建到完成的过程：

- 输入文本和附件。
- 来源消息和回复锚点。
- queued、running、completed、failed、cancelled。
- 使用的 model configuration snapshot。

Submission 不负责发送卡片，也不负责启动具体 Codex/Claude 进程。

### Turn

负责 backend turn 与 submission 的绑定：

- turn started、item 更新、turn completed。
- backend-driven continuation 的绑定。
- 一个 turn 只能有明确的 owner 和终态。

Codex 的 `turn/started`、`item/*`、`serverRequest/resolved`、`turn/completed` 仍由 Codex adapter 按状态机审计处理，再转换成统一的 backend event。

### Interaction

负责待处理的人机交互：

- command approval。
- file approval。
- permissions approval。
- user input 和 MCP elicitation。

`pending -> replied -> resolved` 是 interaction 的状态；Feishu 卡片只是它的一种展示方式。

### ModelConfig

必须明确区分：

```text
DesiredModelConfig   用户保存的目标配置
AppliedModelConfig   backend 已确认应用的配置
TurnModelSnapshot    某个新 turn 实际捕获的配置
```

模型配置的 scope resolution 属于 application/domain policy。单聊值、群组 binding 值、frontend 默认值和当前 turn 快照不能由卡片 renderer 自己推断。

## 5. 统一输入和 effect

所有入口先转换成统一的 application input：

```go
type MessageReceived struct {
    Frontend identity.FrontendID
    Chat     identity.ChatRef
    Message  domain.SubmissionInput
}

type CardActionReceived struct {
    Frontend identity.FrontendID
    Action   domain.CardAction
}

type BackendEventReceived struct {
    Frontend identity.FrontendID
    Event    backend.Event
}
```

use case 不直接调用另一个 service，而是返回 effect：

```go
type Effect interface{ isEffect() }

type SaveSession struct { Session conversation.Session }
type StartTurn struct { Request backend.StartTurnRequest }
type SendMessage struct { Message presentation.Message }
type PatchCard struct { Card presentation.CardPatch }
type ResolveBackendRequest struct { Request backend.ResolveRequest }
```

需要异步执行的 effect 由 runtime runner 处理。Feishu card callback 只做校验、快速持久化或入队，然后立即 ack；慢操作继续走异步 effect 和后续 card patch。

callback 仍然可以存在于 adapter 内部，例如 SDK 的 HTTP handler 或 `state.Store.Update` 的原子 mutate 函数，但不应再作为业务模块之间的隐式控制流。

## 6. Backend 边界

backend adapter 对外只暴露 Feidex 产品需要的能力：

```go
type Gateway interface {
    StartConversation(context.Context, StartConversationRequest) error
    StartTurn(context.Context, StartTurnRequest) error
    SteerTurn(context.Context, SteerTurnRequest) error
    InterruptTurn(context.Context, InterruptTurnRequest) error
    ResolveRequest(context.Context, ResolveRequest) error
}
```

Codex adapter 内部负责：

- JSON-RPC envelope。
- `thread/start`、`thread/resume`、`turn/start`、`turn/steer`、`turn/interrupt`。
- server request 的 reply/resolved 生命周期。
- Codex 专属 item 和 usage 映射。

Claude adapter 内部负责：

- CLI process 和 stream-json。
- session resume。
- control request / response。
- Claude 特有的权限和 plan 原语转换。

application 不应看到 `codexrpc.RequestEnvelope`、Claude raw stream-json、Feishu SDK request 或 card JSON。

不支持的 backend capability 必须通过显式 capability model 表达，不要在 application 中到处增加 `if backend == ...`。

## 7. Feishu 边界

Feishu adapter 负责：

- SDK event 解码。
- message、card action、recall、reaction 转成 application input。
- application effect 转成 Feishu API 调用。
- 卡片 JSON、按钮顺序、消息 patch、限流和权限自愈。

application 只产出语义化 view model 或 effect。例如 primary 切换完成后，application 产出：

```text
SavePrimaryAssignment
PatchGroupMainFeedCard
SendPrimaryStatus
```

Feishu adapter 再决定这些 effect 对应哪个消息、哪个卡片结构和哪个按钮位置。这样“挂在主消息流的群卡片上显示按钮”属于 Feishu presentation 规则，不会渗入 routing 或 primary domain。

菜单、slash command 和 card action 应先解析成同一个 application command。菜单负责展示，command surface 负责解析，use case 负责业务语义，避免菜单入口和命令入口产生两套逻辑。

## 8. 状态存储和并发

`internal/state` 的最终职责应收敛为 storage DTO 和 repository 实现：

```text
domain aggregate
        │
        ▼
repository interface
        │
        ▼
JSON snapshot / future database
```

repository 不发送消息、不渲染卡片、不启动 backend。

每个 frontend/session 应有一个明确的状态转换 owner。可以使用 session actor、keyed dispatcher 或等价机制，让外部事件按 session 串行进入 domain transition。这样 queue、turn lifecycle、approval 和 backend event 不需要通过多个 service callback 互相抢锁。

状态转换应该先持久化领域状态，再执行外部 effect；effect 需要幂等键，以便 Feishu 重投、backend 重连和进程恢复时安全重试。

## 9. 关键流程示例

### 群组 primary 切换

```text
Feishu card action
  -> CardActionReceived
  -> ChangePrimary use case
  -> Routing transition
  -> SavePrimaryAssignment
  -> PatchGroupMainFeedCard
  -> SendPrimaryStatus
```

primary 的 scope、唯一目标 Bot 校验和持久化由 routing/application 负责；按钮结构和消息 patch 由 Feishu adapter 负责。

### 模型配置

```text
/model 或模型卡片
  -> SetModelConfig command
  -> resolve scope and desired config
  -> Save DesiredModelConfig
  -> if safe boundary: ApplyModelConfig
  -> capture TurnModelSnapshot when next local turn starts
  -> render desired/applied/pending status
```

这样“最近已应用模型”不会从另一个 chat scope 的配置字段中读取，显示值和实际 turn snapshot 使用同一套 resolution policy。

### Codex approval

```text
Codex server request
  -> Codex adapter validates protocol state
  -> ApprovalRequested backend event
  -> OpenInteraction use case
  -> SendApprovalCard effect
  -> Feishu card action
  -> ResolveInteraction use case
  -> ResolveBackendRequest effect
  -> ApprovalResolved backend event
  -> resume submission after all requests are resolved
```

协议实现不得把 `serverRequest/resolved` 简化成“用户点击了按钮”。resolved 边界仍由 Codex adapter 和 application interaction 状态共同守护。

## 10. 必须固定的架构规则

建议用 architecture test 和 code review 同时保证以下规则：

- domain 不导入 `internal/app`、`internal/feishu`、`internal/codexrpc`、`internal/state`。
- `internal/feishu` 不导入 `internal/app`；通用字符串、路径和格式化 helper 放在独立低层包。
- backend adapter 不导入 Feishu presentation 或菜单包。
- application 不接收 `*App`，不接收宽泛的 `App` interface，不把 `App` 作为参数继续传递给下层操作。
- application 不暴露 Feishu SDK 类型、Codex RPC envelope 或 Claude raw protocol 类型。
- repository 不执行网络调用，不发送卡片，不修改 runtime process。
- renderer 不读取或修改 session、submission、turn 的业务状态。
- 一个领域状态只有一个 owner；其他模块通过 command/event/effect 访问它。
- 每个菜单能力必须有直接 command 入口；菜单只是 presentation surface。
- 所有 approval、turn、thread、review、compaction、tool input 和 server request 改动必须继续对照状态机审计并补测试。

## 11. 完成标准

最终架构完成时，新增一个业务能力时应能明确回答：

1. 它修改哪个 domain aggregate？
2. 它由哪个 application use case 处理？
3. 它需要哪些 consumer-owned ports？
4. 它产生哪些 effect？
5. 哪个 adapter 执行这些 effect？
6. 它是否触及 Codex/Claude 协议状态机？
7. 它的 frontend、chat 和 session scope 是什么？

同时满足以下条件，才算达到最终目标：

- 生产构造路径完全由 `internal/composition` 负责；`internal/app` 不再创建或缓存 application service、backend client、runtime tracker 或兼容镜像。
- `internal/app` 只绑定 Feishu callback；`internal/feishuapp` 只把 Feishu 事实转换为 typed input，并把 application effects 交给 runtime/adapter；不存在把 `*App` 传入多个子服务的新增路径。
- application、adapter、runtime 和 repository 的依赖方向符合第 2 节，任何跨 owner 控制流都通过显式 port、command、event、query 或 effect。
- 每个 session、submission、turn、interaction 和 frontend runtime 状态只有一个 owner；保存先于外部 effect，重试使用稳定幂等身份。
- backend wire、Feishu SDK、卡片 JSON、CLI stream 和存储 DTO 都停留在对应 adapter/runtime 边界，application 只处理语义化类型。
- 所有涉及 Codex/Claude 状态机的路径继续满足协议审计，且架构测试能够阻止上述边界回退。
