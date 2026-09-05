# Feishu 群多 Bot / 逻辑项目实现说明

状态: 群多 Bot 基础实现完成；Conversation 统一配置模型、BotProfile 私聊配置、FIFO pending queue 和群内统一权限是已确认的后续重构目标

更新时间: 2026-09-05

本文记录 Feishu 群多 Bot / 逻辑项目的产品边界和实现约束。群聊和单聊共享同一个 Conversation 执行模型；单聊是只有一个 Bot、当前 Bot 自动作为 primary 的特殊 Conversation。配置目标仍然有明确区别：单聊用于设置 BotProfile，群聊用于设置当前 Bot 在当前群内的 ConversationBinding 覆盖。

## 1. 产品模型

一个 Feishu 群代表一个逻辑项目。多个不同机器或不同 Feidex 实例上的 Bot 可以加入同一个群：

- Bot A 可以使用本机的 client 目录。
- Bot B 可以使用另一台机器上的 server 目录。
- 这些目录在物理上可以不同，但在群语义上属于同一个逻辑项目。
- 每个 Bot 在群内维护自己的后端上下文，不共享 Codex/Claude thread。
- 明确 `@Bot` 的消息由被 `@` 的 Bot 处理。
- 未明确 `@` 的普通群消息由 primary Bot 处理。

一个 Feishu 单聊也是一个 Conversation，但只包含一个可接收消息的 Bot：

- 当前 Bot 自动被视为该 Conversation 的 primary。
- 单聊不需要 `GroupPrimary`，也不需要用户执行 `/primary`。
- 单聊中的 profile 就是当前 Bot 的 `BotProfile`；单聊是设置 BotProfile 的唯一入口。
- 单聊消息直接进入当前 Bot 的 Conversation queue，执行、pending 和恢复规则与群聊共用。

用户界面不再暴露“绑定”作为产品概念。群内展示和命令只使用以下概念：

- 当前 Bot
- 当前群内工作区 / 当前工作区
- 当前 Bot 在本群内的模型、推理强度、响应速度和运行参数覆盖
- primary
- pending queue 中的原始消息

旧的绑定 slash command 已彻底移除，不再作为本地命令、兼容入口、菜单入口或 help 条目存在。

## 2. 内部概念

### 2.1 Frontend / Bot Instance

`frontend_id` 是 Feidex 运行前端的隔离边界，通常对应一个 Feishu Bot 和它自己的 backend runtime。

frontend 隔离以下内容：

- Session key
- backend runtime
- pending request
- message link
- AgentBinding
- thread / submission 的本地关联

因此同一个群由 Bot A 和 Bot B 分别处理时，仍然是两个不同的本地 Session。

`GroupPrimary` 是例外：它不是 frontend-scoped 状态，而是当前 Feidex 实例内按群共享的一份 owner bot open_id。本机同一 Feidex 进程里的多个 frontend 会读取同一份 `GroupPrimary`，但它们仍用各自 bot open_id 判断自己是否为 primary。

### 2.2 Conversation 与配置目标

Conversation 是所有消息执行流程的统一边界：

```text
Conversation identity = (frontend_id, chat_id)
Conversation kind     = p2p | group  // 仅用于路由策略和展示元数据
```

Conversation 统一拥有消息归一化、submission queue、pending queue、workspace resolution、thread/turn 生命周期和卡片 action 上下文。命令、卡片和队列代码不应直接根据 `ChatType` 分叉；差异应由注入的 scope/policy 提供。

配置目标分为两层：

- `BotProfile`：当前 Bot 的默认 workspace、model、effort、service tier 和运行参数。只能在 p2p Conversation 中设置。
- `ConversationBinding`：群中当前 Bot 的本地覆盖；未设置的运行参数回退到 BotProfile，但 workspace 必须在当前群内单独选择或创建。不同 Bot、不同群分别拥有自己的覆盖。

因此，p2p Conversation profile 自动被视为 BotProfile；群 Conversation profile 只作用于当前 Bot 在当前群内，不修改 BotProfile。

### 2.3 AgentBinding

`AgentBinding` 是当前实现中的内部状态名，目标语义是 `ConversationBinding`：表示当前 frontend 上的 Bot 在某个群 Conversation 中使用哪套本机工作区和运行覆盖。它不是用户概念，也不能出现在普通菜单文案里。

当前内部字段包括：

- `frontend_id`
- `chat_type` / `chat_id`
- 本机 `workspace_id`
- `model_override`
- `reasoning_effort_override`
- `service_tier_override`
- `sandbox_mode_override`
- `approval_policy_override`
- `multi_agent_mode_override`
- `claude_permission_mode`
- `pending` / `active` 状态
- 当前实现的 pending 原消息；目标模型为 FIFO pending queue

当前实现只为群保存 `AgentBinding`，本地状态对 `(frontend_id, group chat_id)` 强制一条记录。统一模型中，ConversationBinding 的身份是 `(frontend_id, chat_id)`；p2p 不必持久化独立 binding，而是把 Conversation profile 直接映射到 BotProfile。多个机器之间没有共享状态库；每个 Feidex 实例只保存自己的本地配置。

### 2.4 GroupPrimary

`GroupPrimary` 是当前 Feidex 实例保存的“某个群的 primary owner bot open_id”本地副本。它不是当前 Bot 自己的 bool 开关，不属于 frontend 隔离状态，也和 `AgentBinding` 没有生命周期依赖：

- `@Bot /primary on` 把被 `@` 的 Bot open_id 写为本群 owner；所有能看到这条群消息的 Feidex 实例都会静默同步自己的本地副本。
- `/primary off` 不再支持；primary owner 只能通过把另一个 Bot 设为 owner 来切换，避免群内被清成无 primary 状态。
- `/primary on` 只写 `GroupPrimary`，不创建或修改 `AgentBinding`。
- 群聊中的 `/workspace`、model、effort、fast 和运行参数配置只写 `AgentBinding`；单聊对应配置写入 `BotProfile`；两者都不隐式切换 primary。
- 当前只支持从 GitHub 线上 snapshot v6 直接升级到包含 `GroupPrimary` 的当前状态；测试环境中间版本不保留兼容迁移。
- 不引入公共存储；不同机器之间只依赖同一条群消息投递到各自 bot 后，各自更新本地 owner 副本。不同机器上的 owner 副本可能短暂不一致，最终以最近一次各实例实际收到并处理的 `@Bot /primary on` 为准。
- 同一 Feidex 实例内的多个 frontend 共享同一份群 owner 副本；非 primary frontend 过滤掉未 `@` 消息，不会影响 primary frontend 自己的 adapter 处理同一条消息。
- 只发送空正文 `@Bot` 不作为 `/primary on` 语法糖；切换 primary 必须显式发送 `@Bot /primary on`。

### 2.5 BotProfile

`BotProfile` 是当前 frontend/Bot 的默认配置，只能在 p2p Conversation 中修改。它不是某个群的配置，也不携带群 primary、群成员或群消息路由状态。

群 Conversation 解析运行参数时，优先使用当前 Bot 在当前群的 ConversationBinding 覆盖；没有覆盖的字段再回退到 BotProfile。一个 BotProfile 可以被该 Bot 参与的多个 p2p Conversation 和多个群 Conversation 继承。

### 2.6 Workspace

Workspace 是当前 frontend 可访问的物理目录及其工作区配置，例如：

- `cwd`
- sandbox
- approval policy
- Claude permission mode

不同机器上的 workspace 可以指向不同目录。Feidex 当前不做跨机器目录同步或一致性校验。

### 2.7 Session

Feidex Session 是“一个 frontend 在一个 Feishu 会话里”的本地状态对象。

Session 身份统一是：

```text
(frontend_id, chat_id)
```

对应的 SessionKey 是：

```text
feishu:frontend:<frontend_id>:chat:<chat_id>
```

其中 `ChatType`、`RootMessageID` 和 `UserID` 仍会保存为 session/submission 的元数据，用于群聊作用域判断、回复续写和消息链接，但不参与 SessionKey 生成。

Session 内可以保存 `BindingID`，但 `BindingID` 只是执行元数据，不能参与 SessionKey 生成。

## 3. 不可违反的约束

### 3.0 Scope invariant

群聊和单聊必须共享 Conversation 的执行、队列、pending、thread、turn、审批和卡片 action 流程。产品差异只允许出现在以下两个策略点：

- p2p 使用当前 Bot 的隐式 primary，配置写入 BotProfile。
- group 使用 primary / mention 路由，配置写入当前 Bot 在当前群内的 ConversationBinding。

群级覆盖不能写入 BotProfile；BotProfile 只能通过 p2p Conversation 修改。命令和菜单的核心处理不得因为 `ChatType` 复制出两套业务实现。

### 3.1 Session identity

必须保持：

```text
session = frontend + chat
```

禁止：

- 使用 `ChatType`、`UserID`、`BindingID` 或 RootMessageID 参与 SessionKey。
- 把不同 frontend 的同一个群合并成一个 Session。
- 因为 workspace、model 或 backend 配置变化而直接改变 SessionKey。
- 让 Bot A 和 Bot B 共用同一个 frontend-scoped Session。

### 3.2 Workspace resolution

群 Conversation 有 `ConversationBinding` 且配置了 workspace 时，群内工作区优先；p2p Conversation 的 workspace profile 直接对应 BotProfile。

如果当前 Bot 在群内尚未配置 workspace，不能静默回退到 BotProfile 的默认 workspace；必须进入 workspace onboarding / 选择流程，或返回明确错误。p2p 没有群级 onboarding 时，可以直接使用 BotProfile 中的 workspace。

Conversation 处于 onboarding pending 状态时，当前 Bot 应处理的普通消息必须按到达顺序进入 pending queue，并展示当前工作区配置卡；只有工作区配置成功并能解析出本机 workspace 后，才按原始 message id、root message id、user、文本和附件元数据顺序重放这些输入。不得只保留最后一条消息。

### 3.3 Group message routing

当前群消息路由规则：

- 当前 Bot 被明确 `@`：接收。
- 普通未 `@` 的顶层群消息：只有 primary Bot 接收。
- 明确 `@` 其他 Bot：当前 Bot 不接收，不能落到 primary Bot。
- 回复当前 Bot 已发送消息：通过 frontend-scoped MessageLink 接收。
- pending 状态不直接投递 Codex/Claude，只展示当前工作区配置入口并暂存原消息。

单聊消息路由规则：

- 当前 Bot 自动是 primary，所有普通消息直接进入当前 Bot 的 Conversation queue。
- 单聊不执行群 primary 初始化，也不需要 `/primary`。
- 单聊中的 mention 元数据不改变接收 Bot，因为不存在其他候选 Bot。

群消息不提供独立的“仅 `@` 才响应”开关。未 `@` 的普通群消息始终只由 primary Bot 处理；明确 `@Bot` 的消息始终只由被 @ 的 Bot 处理。

当前 Bot 是否为 primary 的判断是本地判断：读取当前实例的 `GroupPrimary.OwnerBotOpenID`，再和当前 frontend 的 bot open_id 比较。同一实例内如果 A/B 两个 frontend 都在同一群，A 是 owner，则 A 会处理未 `@` 顶层消息，B 会丢弃；B 的丢弃不会阻止 A，因为二者各自有独立 adapter 和 group policy。

primary 初始化和 `AgentBinding` 无关。Bot 被加入群或首次收到群消息时，Feidex 会用 bot 身份读取群信息里的 `bot_count`：如果 `bot_count == 1`，当前 Bot 的 open_id 自动写为 owner；如果 `bot_count > 1`，先记录“已判断但未设置 owner”。用户显式执行 `@Bot /primary on` 后，所有能收到该群消息的 bot 都会把本地 owner 副本更新为被 `@` 的 Bot open_id；非目标 bot 不回复，也不执行普通命令逻辑。

如果当前 Bot 是 primary，但本群尚未配置 workspace，那么未 `@` 普通消息仍会先进入 workspace onboarding：Feidex 创建 pending `ConversationBinding` queue，按顺序暂存原消息，并在 workspace 配置完成后依次重放这些输入。

### 3.4 Binding-level execution queue

所有 Conversation 的 Session 按 `frontend + chat` 隔离，普通任务执行面按“同一个 Bot 在同一个 Conversation 内”串行：

- 串行 key 是 `frontend_id + chat_id`。
- 同一个 Bot 在同一个 Conversation 里只有一个 chat-scoped session；同一时间只能有一个普通 submission 进入 backend turn。
- 当前 turn 正在执行时，后续普通输入会保留在该 Conversation queue 中；当前 turn 收到最终 `turn/completed` 后，调度器从同一个 queue 继续启动。
- 入队后的 submission 保存自己的 `SessionKey`、Conversation 配置快照和 `WorkspaceID`；消费队列时不能因为 primary、BotProfile 或 workspace 配置变化重新路由。
- Bot A 和 Bot B 可以并行；同一个 Bot 在不同群可以并行；同一个 Bot 在同一个群必须串行。
- 单聊和群聊都遵循相同的 queue 规则；p2p 只是没有其他 Bot 竞争路由。
- `/menu`、`/workspace`、`/model`、`/primary` 等本地控制命令即时处理，不作为普通 backend turn 入队。
- 群 onboarding 期间，所有群内真实用户拥有相同的 Conversation 权限；任何群成员都可以继续提交消息，并按顺序进入 pending queue。pending 卡片、workspace 配置、群级运行参数和群级审批/表单不按最初发送者区分权限。
- 自动重试优先级高于普通输入队列。无论单聊还是群聊，只要 failed turn 已挂起自动重试，同一执行面上的普通 queued submission 都不能启动；自动重试发送的 `继续` 不会消费普通队列，重试循环结束后再恢复队列调度。

当前串行队列仍是运行期队列：session runtime queue、active turn/submission 和 submission runtime state 不写入持久 snapshot。重启后的队列恢复和跨实例队列状态展示是独立后续工作。

### 3.5 Backend state machine

本功能不改变 Codex app-server 的 thread / turn / approval 生命周期：

- Session 仍然是本地状态聚合对象。
- backend thread 的启动、恢复、turn 开始和完成仍遵循现有状态机。
- 群内 workspace 或 runtime 覆盖不能绕过 idle-only 约束。
- 群内 model / effort / service tier / sandbox / approval policy / multi-agent / Claude permission mode 只参与 thread/session start、resume 或 turn start 的 effective config 解析，不改变 Codex app-server 的状态机顺序。
- 如果未来继续扩展群内 backend 参数或改变 thread 复用边界，必须按照 [Codex app-server 状态机审计](codex-app-server-state-machine-audit.md) 增加测试。

## 4. 用户命令和菜单

### 4.1 群内工作区 onboarding

新 Bot 加入群后，用户通过明确 mention 触发本地 onboarding：

```text
@Bot /workspace
@Bot /workspace use WORKSPACE_ID
@Bot /workspace new WORKSPACE_ID CWD
@Bot /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]
```

行为：

- `/workspace` 展示当前 Bot 在本群内的工作区状态。
- `/workspace use` 选择本机已有 workspace。
- `/workspace new` 创建本机 workspace 配置并设为当前群内工作区。
- `/workspace clone` clone 仓库、创建本机 workspace 配置并设为当前群内工作区；表单里可选择 clone 后直接生成 worktree，worktree 默认分支、workspace_id 和目录名使用 bot 显示名 + base project，并通过数字后缀处理同名冲突，不把 frontend id 或 chat id 暴露给用户。
- `/workspace new worktree [BRANCH] [WORKSPACE_ID]` 可从工作区菜单独立打开，也可直接用命令打开，用于基于当前已选本机 Git workspace 创建隔离 worktree；默认命名同样使用 bot 显示名 + base project。
- 工作区未配置前，当前 Bot 保持 pending；pending 状态收到当前 Bot 应处理的普通消息时，只展示 onboarding 卡并暂存原消息，不创建普通 submission。
- 配置成功后，Feidex 按消息到达顺序依次重放 pending queue 中保存的原始消息；pending 期间收到的每一条、由当前 Bot 应处理的普通消息都必须保留，不得只保留最后一条。
- 该流程只修改当前机器的本地状态，不要求其他机器共享 workspace 配置。

### 4.2 群内运行参数

群聊里以下命令都在“当前 Bot、当前群”的作用域内解释；其中 workspace/runtime 命令写入群内覆盖，不修改单聊默认配置或其他 Bot，`/primary on` 只更新群 primary：

```text
/workspace sandbox MODE|default
/workspace policy POLICY|default
/workspace multiagent MODE|default
/workspace permissions MODE|default
/model set MODEL|default
/model effort EFFORT|default
/effort EFFORT|default
/fast fast|default|off|toggle|config
/primary on
```

`@Bot /primary on` 是切换 primary 的唯一命令入口；`/primary off` 不支持，空正文 `@Bot` 也不承担切换语义。

effective-value 优先级：

```text
Session / Thread 临时覆盖
  > 当前 Bot 在当前群内的覆盖
  > 当前 Bot 的 BotProfile
  > Workspace 默认配置
  > frontend / 全局默认配置
```

上面的 `BotProfile` 回退只适用于运行参数。群 Conversation 的 workspace 仍必须先在当前群内完成选择或创建，不能因为 BotProfile 已配置 workspace 就跳过群级 onboarding。

覆盖内容：

- Codex model、reasoning effort、service tier / fast、sandbox、approval policy、multi-agent mode。
- Claude model、Claude permission mode。

### 4.3 菜单边界

群内 `/menu` 规则：

- 未 `@` 发送 `/menu`：由 primary Bot 提供菜单。
- `@BotB /menu`：由 BotB 提供菜单。
- 菜单操作始终只作用于接收并生成这张菜单卡片的本地 Bot。
- 菜单不显示 Bot selector，不通过当前 Bot 的菜单调用其他 Bot。
- 需要操作其他 Bot 时，必须重新发送明确 mention 的命令，例如 `@BotB /menu` 或 `@BotB /workspace`。
- 所有群菜单可达能力都必须有 slash command 入口；不能新增只能通过卡片点击触发的群内配置能力。

当前已实现菜单入口：

```text
/menu
  -> 当前 Bot
      -> 当前工作区 /workspace
          -> 新建工作区 /workspace new
          -> 从仓库创建 /workspace clone
          -> 创建 Worktree /workspace new worktree
      -> 模型配置 /model
      -> 响应速度 /fast config
```

群内 help 也按群作用域改写：它推荐 `/workspace`、`/model`、`/effort`、`/fast` 和 `/primary`，不再展示旧的绑定命令。

### 4.4 单聊与 BotProfile

单聊是只有一个 Bot 的 Conversation，当前 Bot 自动是 primary：

- `/workspace`、`/model`、`/effort`、`/fast` 在单聊中设置当前 Bot 的 BotProfile。
- BotProfile 的 model、effort、service tier、sandbox、approval policy、multi-agent 和 Claude permission 配置可以作为群 ConversationBinding 的运行参数回退值；BotProfile workspace 不跳过群级 onboarding。
- 单聊使用与群聊相同的 submission queue、pending queue、workspace resolution、thread/turn 和 server request 生命周期。
- `/primary` 在单聊中不作为命令处理，因为当前 Bot 已经是隐式 primary。

### 4.5 群内成员权限

群 Conversation 不建立“发起人拥有权”或“首条消息用户”授权模型：

- 所有真实群成员对当前 Bot 的消息提交、workspace/runtime 配置、`/primary on`、pending 卡片、审批、server request 和表单 action 具有相同的 Conversation 级权限。
- pending 消息和卡片 action 必须按 `frontend_id + chat_id` 绑定 Conversation，不按 `UserID` 绑定；配置完成后可由任意真实群成员继续处理该队列。
- Bot 成员不是“真实群成员”，不继承该权限；Bot 之间仍按 primary、mention 和 frontend 隔离规则路由。
- 全局 `allow_from` 等应用入口控制仍可统一限制某个群或某类用户，但不得在同一 Conversation 内额外按最初发送者制造不同权限。

## 5. 已完成实现

- [x] 新增 `AgentBinding` 状态模型。
- [x] 新增 frontend-scoped `BotProfile` 状态模型，并将 p2p 配置映射到当前 Bot profile。
- [x] `AgentBinding` 持久化、frontend scope、chat 查询、删除和深拷贝。
- [x] 新增独立 `GroupPrimary` 状态模型，并改为保存群 owner bot open_id。
- [x] `GroupPrimary` 明确为实例内按群共享的 owner 副本，不按 frontend 隔离。
- [x] primary 自动初始化改为读取 Feishu 群信息 `bot_count`，不再依赖 binding 创建顺序。
- [x] Session 持久化 `BindingID` 元数据。
- [x] Submission 创建时固化 `BindingID` 元数据。
- [x] 群消息路由支持 primary / direct mention / local reply link。
- [x] 未 `@` 消息不会因为提及了其他 Bot 而误落到 primary Bot。
- [x] 同实例多 frontend 中，非 primary frontend 的过滤不会影响 primary frontend 处理未 `@` 消息。
- [x] `@Bot /primary on` 会被所有可见 bot 用于同步本地 owner 副本；非目标 bot 静默处理。
- [x] `/primary off` 不支持；空正文 `@Bot` 不作为 primary 切换入口。
- [x] SessionKey 使用 `frontend + chat`。
- [x] `BindingID` 不参与 SessionKey 推导。
- [x] 群内工作区优先解析。
- [x] 群内工作区为空时阻止静默使用默认 workspace。
- [x] 群内 `/workspace` 可创建 pending 配置，并支持选择已有 workspace、新建 workspace、clone workspace。
- [x] 群内 `/workspace`、`/model`、`/effort`、`/fast` 写入当前 Bot 在当前群内的 workspace/runtime 配置；`/primary` 只写独立 primary 状态。
- [x] 旧的绑定 slash command 已移除，不再进入命令注册、菜单、help 或推荐命令。
- [x] workspace 选择、model、effort、fast 和 workspace runtime 设置的旧菜单按钮，在群聊中写入当前 Bot 的群内配置。
- [x] 删除本机 workspace 配置时，如果仍被某个群内当前 Bot 工作区配置引用，会阻止删除并提示先切换。
- [x] `/menu` 增加“当前 Bot”入口；菜单不提供 Bot selector，也不做跨 Bot handoff。
- [x] 新增/更新 state、appstate、群消息策略、workspace 解析、binding service、群作用域 command/action、effective config、help 和菜单测试。
- [x] 群 workspace selection 改为 Conversation 作用域；p2p workspace selection 可回写 BotProfile。
- [x] pending binding 消息改为 FIFO `PendingMessages`，兼容迁移旧 `PendingMessage`，并支持逐条重放与 recall/reaction 删除。
- [x] 群 Conversation 的 pending request 清除 `OwnerUserID`，允许不同真实群成员继续处理同一 pending action。
- [x] 群消息统一由 app-level primary / mention / reply 路由策略处理，不存在 adapter 级路由开关。

## 6. 后续独立工作

以下设计项不作为本功能完成条件：

- 将当前群专用 `AgentBinding` 重命名并完全收敛为统一的 ConversationBinding 类型名；当前行为已按 Conversation scope 工作。
- 继续清理命令、菜单和 card action 中剩余的历史 `ChatType` 分支，收敛为显式 scope/policy 注入。
- 完善跨进程/跨实例的 pending queue 和 Conversation 配置同步协议。
- 补齐所有 server request / review / plan mode 分支的群成员共享权限回归测试。
- 跨实例共享项目配置、跨 Bot primary 原子切换和全局 Bot 列表。
- 群公告状态条。
- 重启后的本地 runtime queue 恢复和跨实例队列状态展示。
- Bot 离群或 frontend 重装后的本地配置清理。
- `AgentBinding` 生命周期边界，例如删除后已有 Session 是否继续使用已解析 workspace、workspace 变更后已有 backend thread 是否继续复用。

## 7. 验收标准

本功能完成条件：

- [x] 同群不同 RootMessage 共享当前 frontend 的群 session。
- [x] 同群不同 Bot 产生 frontend 隔离的 Session。
- [x] `BindingID` 不出现在 SessionKey 中。
- [x] 每个 Bot 可以使用自己机器上的 workspace。
- [x] 新 Bot 入群后可以通过群内 `/workspace` 选择已有、新建或 clone workspace。
- [x] 未完成 workspace 配置时有明确可操作的提示。
- [x] 群内 model / effort / service tier / sandbox / approval policy / multi-agent / Claude permission mode 参与运行时解析。
- [x] 群内配置命令有明确作用域；`/workspace`、`/model`、`/effort`、`/fast` 写入当前 Bot 的本地群内配置，`/primary` 写入群级 primary 状态。
- [x] 群内 menu 增加当前 Bot 入口，菜单卡片不提供 Bot 选择列表。
- [x] 通过 `@BotB /menu` 或其他明确 mention 命令可以直接进入 BotB 的本地菜单和配置流程。
- [x] 当前新增本地群内配置能力可以从 `/menu` 进入，并有 slash command 入口。
- [x] 同实例多 frontend 中，非 primary frontend 过滤未 `@` 消息不会影响 primary frontend 处理。
- [x] 同一个 Bot 在同一个群内跨 RootMessage 串行执行普通 submission。
- [x] `/primary off` 不支持；空正文 `@Bot` 不作为 primary 切换入口。
- [x] workspace / primary 持久化恢复、workspace 解析、路由和 primary 边界有测试。
- [x] 不破坏现有 Codex app-server thread/turn/approval 状态机。

非本功能验收项：ConversationBinding 与 BotProfile 的统一解析、完整群级状态条、bot 离群清理、重启后的 runtime queue 恢复和跨实例协作协议。

已确认但尚未落地的产品验收项：

- [x] 单聊 Conversation profile 自动作为当前 Bot 的 BotProfile，且单聊是设置 BotProfile 的唯一入口。
- [x] 群 Conversation 使用当前 Bot 的 ConversationBinding 覆盖；未覆盖的运行参数回退到 BotProfile，群 workspace 不跳过本群 onboarding。
- [x] 单聊和群聊复用同一套 queue、pending、thread/turn、审批和 card action 流程；配置目标差异由 scope 处理。
- [x] onboarding 期间 pending queue 严格 FIFO，配置完成后按顺序重放全部消息。
- [x] 群内所有真实用户拥有相同的 Conversation 级 pending/action 权限，且不受最初发送者限制。
- [x] 未 `@` 的普通群消息永远只由 primary Bot 处理；明确 `@Bot` 或回复某 Bot 消息时只路由到对应 Bot。

## 8. 相关实现

- [Session key](../internal/app/appcore/session_key.go)
- [AgentBinding state](../internal/state/store.go)
- [BotProfile state and p2p scope](../internal/app/bot_profile.go)
- [AgentBinding app scope](../internal/app/appstate/agent_binding.go)
- [GroupPrimary state](../internal/app/group_primary.go)
- [Group message routing](../internal/app/group_message_policy.go)
- [Feishu event routing](../internal/app/feishu_event_router.go)
- [Submission metadata](../internal/app/submission/queue.go)
- [Workspace resolution](../internal/app/workspace_selection.go)
- [Effective config](../internal/app/binding_effective.go)
- [Current Bot group config service](../internal/app/binding_service.go)
- [Group-scoped command wrappers](../internal/app/binding_scoped_commands.go)
- [Current Bot menu registration](../internal/app/feature_registry_bindings_binding.go)
- [Workspace/model/fast feature registration](../internal/app/feature_registry_bindings_thread_workspace.go)
- [Workspace card action routing](../internal/app/action_registry_workspace.go)
- [State tests](../internal/state/agent_binding_test.go)
- [Group routing tests](../internal/app/group_message_policy_test.go)
- [Workspace tests](../internal/app/binding_workspace_test.go)
- [Current Bot group config tests](../internal/app/binding_service_test.go)
- [Codex app-server state-machine audit](codex-app-server-state-machine-audit.md)
