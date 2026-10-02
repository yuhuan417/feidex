# 长期架构重构提案

状态：实施中
更新时间：2026-10-02

本文描述 Feidex 的目标架构。设计时暂时忽略现有目录、兼容层和迁移成本，先确定清晰的职责边界，再按阶段迁移现有实现。

本文的结构目标已经获得确认，旧 package boundary 仅供迁移定位，不再限制目标依赖方向。迁移保留 [DEVELOPER.md](../DEVELOPER.md) 的行为契约和 [Codex App Server 状态机审计](codex-app-server-state-machine-audit.md) 的协议约束；thread、turn、approval、server request 和 goal continuation 的行为不能因为重构而改变。

## 1. 要解决的问题

当前代码已经拆成许多子包，但拆分主要发生在文件和服务层面，依赖方向没有形成稳定的层次。典型控制流仍然是：

```text
App
  -> Service
  -> callback / App interface
  -> App
  -> another Service
```

因此 Go 的 import graph 虽然没有直接循环，运行时仍然存在概念循环。接口把循环隐藏起来了，却没有消除循环。

主要表现如下：

| 现象 | 实际问题 |
| --- | --- |
| 根 `internal/app` 持有配置、状态、Feishu、Codex、Claude、队列和生命周期 | `App` 成为所有模块的隐式依赖容器 |
| `submission`、`turnlifecycle`、`convbackend` 都通过宽接口访问宿主 | 子模块仍然通过回调互相调用，职责没有真正分离 |
| `backend` 同时处理 backend 选择、卡片、状态、维护和协议 | backend 包不是协议 adapter，而是第二个业务协调层 |
| session、submission、turn、pending request 分散在多个 store、tracker 和 helper | 同一状态有多个修改入口，没有单一 owner |
| 菜单、slash command、card action 各自连接业务函数 | 同一能力的入口、权限和生效语义容易分叉 |
| `internal/feishu` 反向依赖 `internal/app` 下的工具包 | 下层 adapter 不能独立复用和测试 |
| `appcore` 聚合 config、state、runtime、Codex 和 Feishu 接口 | 公共核心会继续膨胀，成为新的 God package |

目标不是把所有代码机械地移动到更多目录，而是让每个业务状态只有一个 owner，让跨模块控制流变成显式的输入、事件和 effect。

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

## 3. 建议的目录结构

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

这不是要求一次性建立全部目录。它描述的是最终 owner，迁移时可以先在现有 `internal/app` 下实现相同接口，再逐步移动目录。

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

重构时不得把 `serverRequest/resolved` 简化成“用户点击了按钮”。协议 resolved 边界仍由 Codex adapter 和 application interaction 状态共同守护。

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

## 11. 迁移顺序

### 阶段 0：建立约束

- 新增 import architecture tests。
- 定义 `Input`、`BackendEvent`、`Effect` 和 frontend/session identity。
- 禁止新增宽 `App` interface、root compatibility shim 和跨模块业务 callback。

### 阶段 1：抽取领域模型

- 先抽取 session、submission、turn、interaction、model config 的状态和纯转换。
- 保留现有 `state.Store` 作为临时 repository adapter。
- 为关键不变量添加纯 domain tests。

### 阶段 2：统一事件入口

- Feishu message、card action、backend notification、timer 都转换成 application input。
- 现有入口可以继续调用旧实现，但必须经过 dispatcher，逐步减少直接调用根 `App` 方法。

### 阶段 3：迁移低风险边界

- 先迁移 primary、BotProfile、AgentBinding 和 modelconfig。
- 验证群聊/单聊 scope、desired/applied/turn snapshot 和主消息流卡片 patch。

### 阶段 4：迁移 submission、turn 和 interaction

- 把 queue、turn lifecycle、approval、pending form 的状态转换移到 application/domain。
- 用 session actor 或 keyed dispatcher 替代跨 service callback 和分散锁。
- 逐条对照 Codex 状态机审计迁移。

### 阶段 5：形成真正的 backend adapter

- 把 Codex event router 和 Claude stream/control 转换放进 backend adapter。
- application 只消费 backend-neutral event。
- 对 unsupported capability 做显式 capability gate。

### 阶段 6：收敛 Feishu 和 runtime

- Feishu adapter 接管事件解码、卡片渲染和消息发送。
- runtime 接管进程 supervisor、effect runner、startup recovery 和 shutdown。
- 根 `internal/app` 缩小为 composition 和少量跨 owner 编排。

### 阶段 7：删除旧桥接

- 删除 `serviceFor`、宽 `App` interface、owner 间业务 callback 和只转发的 binding 文件。
- 删除重复的状态 facade、backend 分支和旧兼容入口。
- 更新 architecture、backend layering 和 package boundaries 文档。

## 12. 完成标准

重构完成后，新增一个业务能力时应能明确回答：

1. 它修改哪个 domain aggregate？
2. 它由哪个 application use case 处理？
3. 它需要哪些 consumer-owned ports？
4. 它产生哪些 effect？
5. 哪个 adapter 执行这些 effect？
6. 它是否触及 Codex/Claude 协议状态机？
7. 它的 frontend、chat 和 session scope 是什么？

如果一个新功能仍需要把 `*App` 传入多个子服务，或需要在多个 service 之间注册 callback，说明边界还没有收敛完成。

## 13. 实施记录

已落地：

- domain/application 的导入方向守卫与输入/effect 契约。
- primary assignment、primary transition、群消息路由规则，以及 frontend-scoped JSON repository。
- model scope resolution 和统一领域快照；模型应用状态用例；Codex resume config 转换。
- frontend 生命周期、取消、异步任务准入和 shutdown drain。
- submission startup 的纯领域状态和 runtime 并发协调，删除根 App 启动 guard。
- interaction pending/replied/resolved 转换和阻塞恢复规则；JSON DTO adapter 保留 frontend scope。
- conversation/session 类型、backend lineage、workspace 切换/恢复校验与活动操作转换；删除 sessionctx、appcore session facade 和根活动操作转发文件。
- submission aggregate、状态枚举与 queued/running/waiting/terminal 转换已迁入 `internal/domain/submission`；JSON store 只负责持久化和 DTO 拷贝，应用层通过 typed transition 更新状态。
- conversation queue 的去重、FIFO 出队、活动 turn 阻塞和 pending/idle 状态刷新已迁入 `internal/domain/conversation`；队列存取仍由 JSON store 执行，业务规则不再由 storage 方法决定。
- turn lifecycle 编排已迁入 `internal/application/turn`，turn binding tracker 已迁入 `internal/runtime/turnbinding`；旧 `internal/app/turnlifecycle`、`turnbinding`、`usageview` 服务包和 App 生命周期接口已删除。协议 usage 通过 Codex adapter 转换为 domain 值，terminal card 和 usage card 作为 presentation/effect ports 执行。
- Codex lifecycle notification 解码和 request ID 规范化；app router 暂时只负责把 semantic event 交给现有 turn owner。

- Feishu 展示依赖图整体迁入 `internal/adapter/feishu`（approval/forms/cards/delivery/review/thread/turn item），不保留旧 app 展示包。菜单能力声明迁入 application，backend identity/inflight 迁入 domain，运行时值迁入 runtime；删除 menutypes 和 apputil 转发层。

- 全部 36 处 `serviceFor` 与 App 按名称索引的服务缓存已删除。
- submission enqueue/dequeue、启动/失败/回滚、暂存附件和查找 turn 的完整用例迁入 `internal/application/submission`，删除宽 App/PendingQueueApp 接口、旧 app/submission 包与宿主转发适配器。生产队列不再经过 conversation facade 回调自身；Codex 初始化协议与 Claude prompt 编码由 backend adapter 执行。
- workspace 配置值、AgentBinding/BotProfile 和 frontend/session key 解析迁入 domain；标准化 inbound message 由 application 定义，Feishu adapter 不再拥有业务输入类型。存储和 config 保留序列化兼容别名，持久化字段不变。

- Codex runtime recovery/upgrade 已迁入 `internal/runtime/codex`，全局 recovery state 已删除；每个 frontend 单独拥有 client、恢复状态和自动 thread recovery exclusion。新增并验证 frontend 隔离回归测试。

后续迁移状态以本文末尾“核心边界整块迁移”记录为准；提案仍在实施中。

### 2026-10-02 核心边界整块迁移

- conversation 创建、恢复、显式选择、fork、interrupt 和 continue 用例迁入 application；Codex/Claude gateway 只执行外部操作。删除 convbackend、conversation backend facade、workspace thread service 及回调自己的 queue 路径。
- dispatcher 覆盖 Feishu message、card action、recall、reaction、Codex notification/server request 和 retry timer；跨 frontend 输入在调用 owner 前被拒绝。重试定时器携带 generation token，过期回调不会启动 submission。
- Codex adapter 完成 notification/request 解码、numeric/string request ID 保真和 protocol rejection；application 消费 semantic event，删除两层旧 event router。
- 自动重试状态、退避和定时 dispatch 归属 `internal/runtime/autoretry`，卡片和入口归属 Feishu adapter，删除旧宽 App 接口。
- startup recovery 与 submission runtime cleanup 归属 `internal/runtime/maintenance`；原维护服务只保留环境清理和升级查询，使用固定依赖，不再访问宿主。
- standalone compaction 生命周期归属 application，协议调用归属 Codex adapter，卡片归属 Feishu adapter，删除 compact 到 App 的回调链。
- Claude runtime 不再持有 App；Claude catalogue/history 位于 backend adapter。turnstream 和 finalcardpatch 位于 Feishu adapter，删除宿主访问器及纯转发包装。
- effect runner 已执行实际 SendMessage/PatchCard，保存或外部 effect 失败会阻止后续 effect；其余同步端口仍需收敛为 effect。
- session snapshot 复制与 active-work 判定归属 conversation domain。显式 resume 保存失败时保留调用方旧 lineage，不发布 live thread。
- 回归覆盖 frontend 隔离、cancelled effect、save-before-start、request ID 保真、workspace 拒绝和 conversation persistence failure。原审批、模型、菜单、review、goal 和 compaction 契约继续运行。

### 2026-10-02 前端服务边界继续收敛

- history command/history backend 已分别迁入 Feishu adapter 与 Codex history adapter，删除 history 宿主 facade 和 backend callback。
- upgrade service 改为显式依赖集合，取消宽宿主接口和 appcore 生命周期耦合。
- planmode 与 goalcmd 改为 consumer-owned Dependencies，composition root 只在绑定处组装 capability ports；删除对应宽 App interface 和 adapter。
- 保持模型生效、thread 生命周期、pending 状态、菜单顺序与 frontend 隔离契约不变；全量 Go 测试 1215 项通过。
- workspacecmd 的配置、管理和渲染服务改为显式 workspace capability carrier，删除其宿主 App interface；MCP bridge 改为显式 Dependencies，删除 MCP 宿主 adapter。

当前剩余：遗留 message/card command 编排，thread/debug/review 的宽宿主接口，backend permission driver 的宿主动态回查，Claude stream presentation 回调，完整 session event 串行 owner，effect pipeline 的同步端口，以及 composition root 的最终收敛。以上未完成前保持“实施中”。
