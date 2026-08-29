# Feishu 群多 Bot / 逻辑项目实现说明

状态: 当前实现完成；跨实例共享配置、BotProfile 继承、多人审批权限模型和群公告状态条是独立后续工作

更新时间: 2026-08-25

本文记录 Feishu 群多 Bot / 逻辑项目的最终产品边界和实现约束。核心原则是：本功能的行为变化只影响群聊；单聊体验保持原样。群聊里同一套菜单和 slash command 仍可用，但配置作用域变为“当前接收命令的 Bot 在当前群内”。

## 1. 产品模型

一个 Feishu 群代表一个逻辑项目。多个不同机器或不同 Feidex 实例上的 Bot 可以加入同一个群：

- Bot A 可以使用本机的 client 目录。
- Bot B 可以使用另一台机器上的 server 目录。
- 这些目录在物理上可以不同，但在群语义上属于同一个逻辑项目。
- 每个 Bot 在群内维护自己的后端上下文，不共享 Codex/Claude thread。
- 明确 `@Bot` 的消息由被 `@` 的 Bot 处理。
- 未明确 `@` 的普通群消息由 primary Bot 处理。

用户界面不再暴露“绑定”作为产品概念。群内展示和命令只使用以下概念：

- 当前 Bot
- 当前群内工作区 / 当前工作区
- 当前 Bot 在本群内的模型、推理强度、响应速度和运行参数覆盖
- primary
- pending 原消息

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

### 2.2 AgentBinding

`AgentBinding` 是内部状态名，表示“当前 frontend 上的 Bot 在某个群内使用哪套本机工作区和运行覆盖”。它不是用户概念，也不能出现在普通菜单文案里。

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
- pending 原消息

当前本地状态对 `(frontend_id, chat_type, chat_id)` 强制一条 `AgentBinding`。多个机器之间没有共享状态库；每个 Feidex 实例只保存自己的本地配置。

### 2.3 GroupPrimary

`GroupPrimary` 是当前 Feidex 实例保存的“某个群的 primary owner bot open_id”本地副本。它不是当前 Bot 自己的 bool 开关，不属于 frontend 隔离状态，也和 `AgentBinding` 没有生命周期依赖：

- `@Bot /primary on` 把被 `@` 的 Bot open_id 写为本群 owner；所有能看到这条群消息的 Feidex 实例都会静默同步自己的本地副本。
- `/primary off` 不再支持；primary owner 只能通过把另一个 Bot 设为 owner 来切换，避免群内被清成无 primary 状态。
- `/primary on` 只写 `GroupPrimary`，不创建或修改 `AgentBinding`。
- `/workspace`、model、effort、fast 和运行参数配置只写 `AgentBinding`，不隐式切换 primary。
- 当前只支持从 GitHub 线上 snapshot v6 直接升级到包含 `GroupPrimary` 的当前状态；测试环境中间版本不保留兼容迁移。
- 不引入公共存储；不同机器之间只依赖同一条群消息投递到各自 bot 后，各自更新本地 owner 副本。不同机器上的 owner 副本可能短暂不一致，最终以最近一次各实例实际收到并处理的 `@Bot /primary on` 为准。
- 同一 Feidex 实例内的多个 frontend 共享同一份群 owner 副本；非 primary frontend 过滤掉未 `@` 消息，不会影响 primary frontend 自己的 adapter 处理同一条消息。
- 只发送空正文 `@Bot` 不作为 `/primary on` 语法糖；切换 primary 必须显式发送 `@Bot /primary on`。

### 2.4 Workspace

Workspace 是当前 frontend 可访问的物理目录及其工作区配置，例如：

- `cwd`
- sandbox
- approval policy
- Claude permission mode

不同机器上的 workspace 可以指向不同目录。Feidex 当前不做跨机器目录同步或一致性校验。

### 2.5 Session

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

本功能的实现边界只覆盖群聊作用域配置。单聊窗口里的 `/workspace`、`/model`、`/effort`、`/fast`、菜单结构和既有 thread 语义必须保持原有行为。群聊新增的“当前 Bot 在本群内”作用域不能泄漏到单聊默认配置。

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

有 `BindingID` 且对应 `AgentBinding` 配置了 workspace 时，群内工作区优先。

如果当前 Bot 在群内尚未配置 workspace，不能静默回退到全局默认 workspace；必须进入 workspace onboarding / 选择流程，或返回明确错误。

pending 状态收到当前 Bot 应处理的普通群消息时，必须先暂存原始 `InboundMessage` 并展示当前工作区配置卡；只有工作区配置成功并能解析出本机 workspace 后，才用原始 message id、root message id、user、文本和附件元数据重放该输入。

### 3.3 Group message routing

当前群消息路由规则：

- 当前 Bot 被明确 `@`：接收。
- 普通未 `@` 的顶层群消息：只有 primary Bot 接收。
- 明确 `@` 其他 Bot：当前 Bot 不接收，不能落到 primary Bot。
- 回复当前 Bot 已发送消息：通过 frontend-scoped MessageLink 接收。
- pending 状态不直接投递 Codex/Claude，只展示当前工作区配置入口并暂存原消息。

当前 Bot 是否为 primary 的判断是本地判断：读取当前实例的 `GroupPrimary.OwnerBotOpenID`，再和当前 frontend 的 bot open_id 比较。同一实例内如果 A/B 两个 frontend 都在同一群，A 是 owner，则 A 会处理未 `@` 顶层消息，B 会丢弃；B 的丢弃不会阻止 A，因为二者各自有独立 adapter 和 group policy。

primary 初始化和 `AgentBinding` 无关。Bot 被加入群或首次收到群消息时，Feidex 会用 bot 身份读取群信息里的 `bot_count`：如果 `bot_count == 1`，当前 Bot 的 open_id 自动写为 owner；如果 `bot_count > 1`，先记录“已判断但未设置 owner”。用户显式执行 `@Bot /primary on` 后，所有能收到该群消息的 bot 都会把本地 owner 副本更新为被 `@` 的 Bot open_id；非目标 bot 不回复，也不执行普通命令逻辑。

如果当前 Bot 是 primary，但本群尚未配置 workspace，那么未 `@` 普通消息仍会先进入 workspace onboarding：Feidex 创建 pending `AgentBinding`、暂存原消息，并在 workspace 配置完成后重放该原始输入。

### 3.4 Binding-level execution queue

群聊里的 Session 按 `frontend + chat` 隔离，普通任务执行面也按“同一个 Bot 在同一个群内”串行：

- 串行 key 是 `frontend_id + group chat_id`。
- 同一个 Bot 在同一个群里只有一个 chat-scoped session；同一时间只能有一个普通 submission 进入 backend turn。
- 当前 turn 正在执行时，后续普通输入会保留在该 group session queue 中；当前 turn 收到最终 `turn/completed` 后，调度器从同一个 session queue 继续启动。
- 入队后的 submission 保存自己的 `SessionKey`、`BindingID` 和 `WorkspaceID`；消费队列时不能因为 primary 或 workspace 配置变化重新路由。
- Bot A 和 Bot B 可以并行；同一个 Bot 在不同群可以并行；同一个 Bot 在同一个群必须串行。
- `/menu`、`/workspace`、`/model`、`/primary` 等本地控制命令即时处理，不作为普通 backend turn 入队。
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
- 配置成功后，Feidex 自动用暂存的原始消息继续处理输入；如果 pending 期间又收到新的普通消息，以最后一条暂存消息为准。
- 该流程只修改当前机器的本地状态，不要求其他机器共享 workspace 配置。

### 4.2 群内运行参数

群聊里以下命令写入“当前 Bot 在当前群内”的运行覆盖，不修改单聊默认配置，也不修改其他 Bot：

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
  > Workspace 默认配置
  > frontend / 全局默认配置
```

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

### 4.4 单聊不变

单聊场景仍保持原有行为：

- `/workspace` 管理当前 bot/frontend 的普通工作区配置。
- `/model`、`/effort` 管理当前 bot/frontend 的默认模型配置。
- `/fast` 仍按当前 thread 的响应速度语义运行。
- `/primary` 在单聊中不作为本地群配置命令处理；群内 primary 状态按 owner bot open_id 判断。

## 5. 已完成实现

- [x] 新增 `AgentBinding` 状态模型。
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
- [x] pending 状态收到普通群消息时先暂存原始消息并展示当前工作区卡，配置成功后自动重放原始消息。
- [x] `/menu` 增加“当前 Bot”入口；菜单不提供 Bot selector，也不做跨 Bot handoff。
- [x] 新增/更新 state、appstate、群消息策略、workspace 解析、binding service、群作用域 command/action、effective config、help 和菜单测试。

## 6. 后续独立工作

以下设计项不作为本功能完成条件：

- Bot 私聊 BotProfile 的持久化模型与 profile -> `AgentBinding` 继承。
- 跨实例共享项目配置、跨 Bot primary 原子切换和全局 Bot 列表。
- 群公告状态条。
- 多自然人的审批 / server request 权限模型。
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
- [x] 群内配置命令有明确作用域；`/workspace`、`/model`、`/effort`、`/fast`、`/primary` 写入当前 Bot 的本地群内配置。
- [x] 群内 menu 增加当前 Bot 入口，菜单卡片不提供 Bot 选择列表。
- [x] 通过 `@BotB /menu` 或其他明确 mention 命令可以直接进入 BotB 的本地菜单和配置流程。
- [x] 当前新增本地群内配置能力可以从 `/menu` 进入，并有 slash command 入口。
- [x] 同实例多 frontend 中，非 primary frontend 过滤未 `@` 消息不会影响 primary frontend 处理。
- [x] 同一个 Bot 在同一个群内跨 RootMessage 串行执行普通 submission。
- [x] `/primary off` 不支持；空正文 `@Bot` 不作为 primary 切换入口。
- [x] workspace / primary 持久化恢复、workspace 解析、路由和 primary 边界有测试。
- [x] 不破坏现有 Codex app-server thread/turn/approval 状态机。

非本功能验收项：BotProfile 私聊配置继承、完整群级状态条、多人审批权限模型、bot 离群清理、重启后的 runtime queue 恢复和跨实例协作协议。

## 8. 相关实现

- [Session key](../internal/app/appcore/session_key.go)
- [AgentBinding state](../internal/state/store.go)
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
