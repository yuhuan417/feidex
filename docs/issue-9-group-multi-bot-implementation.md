# Issue 9: Feishu 群多 Bot / 逻辑项目实现说明

状态: issue 9 实现完成；跨实例共享配置、BotProfile 继承、多人输入队列和群公告状态条是独立后续 issue

更新时间: 2026-08-23

本文是 issue 9 的实现约束和进度记录。它描述当前代码应该遵守的语义，并明确区分本 issue 已完成范围与独立后续 issue。

## 1. 目标与范围

一个 Feishu 群代表一个逻辑项目。多个运行在不同 Feidex 实例或不同机器上的 bot 可以加入同一个群：

- Bot A 可以使用本机的 client 目录。
- Bot B 可以使用另一台机器上的 server 目录。
- 两个目录可以不同，但在群语义上属于同一个逻辑项目。
- 每个 bot 在群内维护自己的后端上下文，不共享 Codex/Claude thread。
- 未明确 `@` 某个 bot 的普通群消息，由主 bot 处理。
- 明确 `@` 某个 bot 的消息，由被 `@` 的 bot 处理。

本 issue 解决群、bot、binding、session、workspace 之间的基础关系，以及当前 bot 的本地 onboarding、菜单入口、binding 级运行参数和旧命令在 binding mode 下的作用域收敛。多人输入队列、多个 bot 的跨 bot 串行调度、群公告状态条、私聊 BotProfile 继承等属于其他 issue，不在本文的完成判定内。

## 2. 核心概念

### 2.1 Logical Project / Chat

Feishu 群是逻辑项目的载体。群本身不是某台机器上的 workspace，也不是某个 bot 的后端 thread。

当前没有共享的中央项目配置源。不同 Feidex 实例只通过 Feishu 群和消息语义形成逻辑上的同一项目；本地 workspace、模型和运行时状态仍由各实例独立保存。

### 2.2 Frontend / Bot Instance

`frontend_id` 是一个 Feidex 运行前端的隔离边界，通常对应一个 Feishu bot 和它自己的 backend runtime。

frontend 隔离以下内容：

- session key
- backend runtime
- pending request
- message link
- agent binding
- thread / submission 的本地关联

因此同一个群、同一个 RootMessage，由 Bot A 和 Bot B 分别处理时，仍然是两个不同的本地 Session。

### 2.3 Binding

`AgentBinding` 表示“当前本地 frontend 上的 bot 如何接入某个逻辑群项目”。它是本地路由和执行配置，不是对话身份。

当前数据模型包括：

- `frontend_id`
- `chat_type` / `chat_id`
- `component`，例如 `client`、`server`
- 本地 `workspace_id`
- `model_override`
- `reasoning_effort_override`
- `service_tier_override`
- `sandbox_mode_override`
- `approval_policy_override`
- `multi_agent_mode_override`
- `claude_permission_mode`
- `primary`
- `pending` / `active` 状态

当前本地状态对 `(frontend_id, chat_type, chat_id)` 强制一个 binding。多个机器上的多个 bot 不依赖共享状态库，而是每个 frontend 在自己的本地 state 中保存自己的 binding。

Binding **不能**放进 SessionKey，也不能用来合并一个群中的不同 RootMessage。

### 2.4 Workspace

Workspace 是当前 frontend 可访问的物理目录及其工作区配置，例如：

- `cwd`
- sandbox
- approval policy
- Claude permission mode
- workspace 默认模型等工作区级参数

不同机器上的 workspace 可以指向不同目录。它们是否对应同一个代码项目，由部署和用户配置保证，Feidex 当前不做跨机器目录同步或一致性校验。

### 2.5 Session

Feidex Session 是“一条 Feishu 对话分支”的本地状态对象。

群聊 Session 的身份是：

```text
(frontend_id, chat_type, chat_id, root_message_id)
```

对应的 SessionKey 是：

```text
feishu:frontend:<frontend_id>:group:<chat_id>:root:<root_message_id>
```

其中 `root_message_id` 是 Feishu 回复树的根消息；如果入口消息没有显式 RootMessageID，当前实现使用入口消息自己的 MessageID 作为 root。

Session 内可以保存：

- 当前 backend thread
- 当前 turn / submission
- 输入队列和 active operation
- 审批、用户输入等 pending 状态的关联
- Feishu 消息与 turn 的 link
- `BindingID` 等本地执行元数据

Session 不等于：

- Feishu 群
- Binding
- Workspace
- Codex/Claude thread
- 单条消息或单个 turn

同一根消息树内的多条消息可以共享一个 Session；同一群中两个不同根消息必须创建两个 Session。

单聊沿用现有语义：

```text
(frontend_id, chat_id, user_id)
```

### 2.6 Backend Thread

Codex thread 或 Claude session 是后端上下文。Feidex Session 管理它们，但不把后端 thread 当作 Feishu Session 的身份。

正常情况下，一个 Feishu Session 会沿用自己的 backend thread；执行 `/thread new`、`/thread fork`、workspace 切换或 backend 特定的恢复流程时，Session 内的 thread lineage 可能变化，但 SessionKey 不因此改变。

## 3. 不可违反的约束

### 3.1 Session identity

必须保持：

```text
group session = frontend + chat + RootMessage
```

禁止：

- 使用 BindingID 替代 RootMessageID。
- 把整个群合并成一个 Session。
- 因为 workspace、model 或 backend 配置变化而直接改变 SessionKey。
- 让 Bot A 和 Bot B 共用同一个 frontend-scoped Session。

### 3.2 Binding lookup

Binding 的正确使用方式是：

1. 先根据当前 frontend 和当前 chat 查询本地 binding。
2. 创建新的 Session 时，把 BindingID 写入 Session 元数据。
3. 创建 Submission 时，把 BindingID 固化到 Submission 元数据。
4. 根据 Session 的 BindingID 解析本地 workspace。

Binding lookup 不能参与 SessionKey 生成。

当前实现对已有 Session 的行为是：如果 Session 已经有 BindingID，则沿用该 ID；如果没有，则在入队时从当前 chat 的本地 binding 补齐。Binding 记录本身按 ID 动态查找，因此修改 binding 的 workspace / model / runtime override 会影响后续提交的 effective config；已经在运行中的 turn 不被中途改写，已有 active backend thread 仍按现有 thread 生命周期收口。

### 3.3 Workspace resolution

有 BindingID 且 binding 配置了 workspace 时，binding workspace 优先。

有 binding 但 workspace 为空时，不能静默回退到全局默认 workspace；必须进入后续的 workspace onboarding / 选择流程，或返回明确错误。

没有 binding 的旧群，继续保留原有默认 workspace 和 `group_at_only` 兼容行为。

### 3.4 Group message routing

当前 active binding 模式下：

- 当前 bot 被明确 `@`：接收。
- 普通未 `@` 的顶层群消息：只有 primary bot 接收。
- 明确 `@` 其他 bot：当前 bot 不接收，不能落到 primary bot。
- 回复当前 bot 已发送消息：通过 frontend-scoped MessageLink 接收。
- `@everyone`：沿用 `RespondToAtEveryone` 配置，并要求本地 primary binding 才能作为默认处理者。

pending binding 仍允许直接 `@` 当前 bot，以便完成 onboarding；未完成 onboarding 前不会接收普通未 `@` 消息。

### 3.5 Backend state machine

本 issue 不改变 Codex app-server 的 thread / turn / approval 生命周期：

- Session 仍然是本地状态聚合对象。
- backend thread 的启动、恢复、turn 开始和完成仍遵循现有状态机。
- binding、workspace 或 frontend 配置不能绕过 idle-only 约束。
- binding 级 model / effort / service tier / sandbox / approval policy / multi-agent / Claude permission mode 只参与 thread/session start、resume 或 turn start 的 effective config 解析，不改变 Codex app-server 的状态机顺序。
- 如果未来继续扩展 binding 级 backend 参数或改变 thread 复用边界，必须按照 [Codex app-server 状态机审计](codex-app-server-state-machine-audit.md) 增加测试。

## 4. 实现进度

### 4.1 已完成

- [x] 新增 `AgentBinding` 状态模型。
- [x] binding 持久化、frontend scope、chat 查询、删除和深拷贝。
- [x] snapshot version 从 6 迁移到 8。
- [x] Session 持久化 `BindingID` 元数据。
- [x] Submission 创建时固化 `BindingID` 元数据（Submission 当前仍是运行时状态，不写入 snapshot）。
- [x] 群消息路由支持 primary / direct mention / local reply link。
- [x] 未 `@` 消息不会因为提及了其他 bot 而误落到 primary bot。
- [x] SessionKey 恢复为 `frontend + chat + RootMessage`。
- [x] 移除 binding-only SessionKey 及从 SessionKey 推导 BindingID 的逻辑。
- [x] binding workspace 优先解析。
- [x] binding workspace 为空时阻止静默使用默认 workspace。
- [x] 群内 `/bind` 可创建 pending binding，并支持绑定已有 workspace、新建 workspace、clone workspace、设置 component、primary 和 binding 级运行参数。
- [x] 已有 binding 的群内 `/workspace use`、`/workspace sandbox/policy/multiagent/permissions`、`/model set`、`/model effort`、`/effort` 和 `/fast` 会写入当前 bot 的本地 binding，而不是无作用域地切换 session workspace 或全局模型配置。
- [x] workspace 选择、model、effort、fast 和 workspace runtime 设置的旧菜单按钮，在 binding mode 下同样写入当前 bot 的 binding。
- [x] 删除本机 workspace 配置时，如果仍被当前 frontend 的群 binding 引用，会阻止删除并提示先切换 binding。
- [x] binding 级 model / effort / service tier / sandbox / approval policy / multi-agent / Claude permission mode 已参与 Codex 和 Claude 的 effective config 解析。
- [x] `/menu` 增加“当前 Bot”入口，当前 bot 的 binding、workspace、model 和 fast 配置可以从菜单进入；菜单不提供 bot selector，也不做跨 bot handoff。
- [x] 增加 state、appstate、群消息策略、workspace 解析、binding service、binding-scoped command/action、effective config 和菜单测试。
- [x] `go test ./...` 已通过。

### 4.2 独立后续 issue

以下设计项保留为后续 issue，不作为 issue 9 完成条件：

- `AgentBinding.Component` 目前主要是描述信息，尚未形成 client/server 专用能力路由。
- Bot 私聊 BotProfile 的持久化模型与 profile -> binding 继承。
- 跨实例共享项目配置、跨 bot primary 原子切换和全局 bot 列表。
- 群公告状态条和多人输入队列。

## 5. 已实现命令与边界

### 5.1 新 bot 入群 onboarding

本 issue 不接入群成员变更事件。新 bot 加入群后，用户通过明确 mention 触发本地 onboarding：

```text
@Bot /bind
@Bot /bind use WORKSPACE_ID
@Bot /bind new WORKSPACE_ID CWD
@Bot /bind clone GIT_URL [WORKSPACE_ID] [--parent DIR]
```

行为：

- `/bind` 或 `/bind status` 会在当前群创建或展示当前 frontend 的 pending binding。
- `/bind use` 绑定本机已有 workspace，并把 binding 激活。
- `/bind new` 创建本机 workspace 配置并绑定当前群。
- `/bind clone` clone 仓库、创建本机 workspace 配置并绑定当前群。
- workspace 未绑定前，该 binding 保持 pending；pending binding 只接受明确 `@Bot` 的 onboarding 命令，不接收未 `@` 的普通群消息。
- 该流程只修改当前机器的本地 binding，不要求其他机器共享 workspace 配置。

菜单入口：`/menu` -> `当前 Bot` -> `群内 Binding /bind`。菜单只作用于生成这张卡片的 bot，不显示 bot 列表。

### 5.2 Binding 级运行参数

已实现 effective-value 优先级：

```text
Session / Thread 临时覆盖
  > 群内当前 bot binding 覆盖
  > Workspace 默认配置
  > frontend / 全局默认配置
```

覆盖：

- Codex model、reasoning effort、service tier / fast、sandbox、approval policy、multi-agent mode。
- Claude model、Claude permission mode。

命令入口：

```text
/bind model MODEL|default
/bind effort EFFORT|default
/bind fast fast|default|off
/bind sandbox MODE|default
/bind policy POLICY|default
/bind multiagent MODE|default
/bind permissions MODE|default
```

这些配置是当前 frontend 对当前群的本地 binding override；不修改其他 bot，也不修改共享项目配置。

### 5.3 群内命令和 Menu 作用域

菜单约束：

- 未 `@` 发送 `/menu`：由 primary bot 提供菜单。
- `@BotB /menu`：由 BotB 提供菜单。
- 菜单操作始终只作用于接收并生成这张菜单卡片的本地 bot。
- 菜单不显示 bot selector，不通过当前 bot 的菜单调用其他 bot。
- 需要操作其他 bot 时，必须重新发送明确 mention 的命令，例如 `@BotB /menu` 或 `@BotB /bind`。

当前菜单已增加“当前 Bot”一级入口，并提供“群内 Binding /bind”、Binding Workspace、Binding 模型、Binding 响应速度等入口。进入 binding mode 后，Workspace、Model、Fast 相关旧菜单按钮会按当前 bot 的本地 binding 作用域处理；Thread、Review、Skills、Status 等能力继续按当前 session/thread/workspace 语义运行。后续如果继续重组一级菜单结构，仍必须保持每个菜单能力都有直接 slash command 入口。

## 6. 后续工作

### 后续 issue: Bot 私聊配置继承

设计上，bot 私聊是配置该 bot 能力的地方；加入群后，binding 可以继承这些默认能力并允许针对某群覆盖。

当前尚未实现：

- bot 私聊配置 profile 的持久化模型。
- profile 到 binding 的继承关系。
- binding 覆盖和 profile 更新后的生效边界。
- 配置变更对已有 Session / backend thread 的影响。

### 本 issue 已固定的群内命令和 UI 边界

需要把现有命令区分为不同作用域，并补齐群内入口：

| 配置对象 | 典型内容 | 当前状态 |
| --- | --- | --- |
| 群 / 逻辑项目 | 群成员 bot、primary、binding 列表、项目级展示 | primary 和本地 binding 已实现；跨实例 bot 列表/群级展示是后续 issue |
| 当前 bot 在群内的 binding | workspace、component、model/effort 覆盖 | 已有 `/bind`、`/workspace`、`/model`、`/effort`、`/fast` 和菜单入口 |
| 当前 Session / Thread | thread、临时模型、turn 生命周期、审批状态 | 现有功能继续按 session/thread 语义运行 |
| 当前 frontend | backend runtime、Feishu app、全局默认配置 | 已有功能，不在本 issue 重新建模 |

必须避免在群里提供一个无作用域的 `/workspace use`，让不同 bot 或不同 RootMessage 互相覆盖 workspace。当前实现采用兼容策略：老群没有本地 binding 时继续沿用旧命令语义；一旦通过 `/bind` 进入 binding mode，群内 `/workspace use` 和 workspace runtime 配置就作用于“当前 bot 的 binding”，不再切换 session workspace。

### 后续 issue: 多 Bot 场景下的 Menu 组织增量

当前菜单已经会根据 Codex / Claude backend 动态隐藏或改写部分条目，但多 bot 群不能只继续增加 backend 条件。菜单必须先按作用域组织，再在作用域内部按当前 bot 的 backend 和能力过滤。

后续可继续把群内菜单整理为以下一级结构。菜单卡片只服务于生成它的当前 bot，不在菜单内选择其他 bot：

```text
项目（群级）
当前 Bot
当前对话
工具
管理员工具
```

各分组的语义约束：

- `项目（群级）` 只展示群/逻辑项目状态、公告状态和可获得的群级 bot 概览；不能放某个 bot 的 workspace、model 或 thread 操作。
- `当前 Bot` 针对生成这张菜单卡片的 bot 在当前群的 binding，包括 binding 状态、workspace、model/effort、fast、quiet 和 bot 状态；菜单内不显示 bot 选择列表，也不通过菜单切换 bot。
- `当前对话` 针对当前 `RootMessage` 对应的 Session 及其 backend Thread/Claude Session，包括 new、fork、resume、history、usage、plan、goal、compact、stop 和对话级权限。
- `工具` 放 review、skills、download 等依赖当前 bot 本地 workspace 的能力。
- `管理员工具` 放 backend、debug、upgrade、Codex/Claude CLI 等 frontend 或本机级操作，默认不在普通群菜单中展示。

Backend 动态过滤仍然保留，但只影响 `当前 Bot` 和 `当前对话` 内部的能力：

- Codex 可显示 Thread、Plan、Goal、Review、Skills、Fast、sandbox/policy。
- Claude 可显示 Session、Session permissions 和 Claude 对应的 workspace permissions。
- 两端共有能力使用稳定的产品名称，例如“当前对话”“历史记录”“停止任务”“模型配置”，不应让整个一级菜单随 backend 完全变形。

群内 `/menu` 的目标规则：

- 未 `@` 发送 `/menu`：由 primary bot 提供菜单。
- `@BotB /menu`：由 BotB 提供菜单。
- 菜单操作始终只作用于接收并生成这张菜单卡片的本地 bot，不支持在菜单中选择或调用其他 bot。
- 菜单子页、表单、确认卡和异步结果都沿用这张卡片所属 bot 的本地 frontend、binding、workspace 和 Session 上下文。
- 需要操作其他 bot 时，必须重新发送带明确 mention 的命令，例如 `@BotB /menu` 或 `@BotB /bind`；不通过当前菜单转发，也不依赖 bot 发现或跨 frontend 交接协议。
- 菜单卡片标题和状态可以展示当前 bot、component、binding、workspace 和 backend，但不需要展示可选 bot 列表。

菜单是所有 Feidex 用户能力的统一入口。每个可由用户使用的本地能力都必须同时满足：

- 可以从 `/menu` 的某个作用域进入；
- 有对应的 slash command 或等价直接入口；
- 进入菜单后能看到当前 bot、当前 binding、当前 Session/Thread 等实际上下文；
- backend 不支持的能力要明确隐藏或展示不可用原因，不能悄悄变成无作用域的操作。

菜单 action 的上下文必须和作用域一致，并且使用生成卡片的本地 bot 上下文：

```text
群级菜单       -> chat_id
Binding 菜单   -> binding_id + chat_id
Session 菜单   -> session_key
Frontend 菜单  -> frontend_id
```

Binding 菜单不能只依赖 `session_key`，Session 菜单也不能通过修改 SessionKey 来切换 binding。菜单 action 不得把 `BindingID` 或 bot 标识拼进 SessionKey。`BindingID` 仍然不能进入 SessionKey。

后续菜单注册项至少需要声明以下元数据：`scope`、`backend`、`requires`、`visibility` 和 `mutating`。渲染顺序应为：先按群聊/私聊和作用域过滤，再按 backend 能力过滤，最后根据 binding、workspace、active Session/Thread 和管理员权限决定是否展示。

### 后续 issue: Binding lifecycle 和配置变更

需要补齐以下行为定义和测试：

- binding 删除后，已有 Session 是否继续使用已解析的 workspace。
- binding workspace 变更后，已有 Session 是否允许继续当前 backend thread。
- binding 从 pending 变为 active 的原子边界。
- primary 变更时，未 `@` 的新消息如何切换处理者。
- bot 离开群或 frontend 重装后的 binding 清理。
- 同一群多个 frontend 的 binding 是否允许不同 component 和不同 workspace。

### P2: 观测和恢复

需要让状态卡或群公告能够明确展示：

- 当前群有哪些 bot binding。
- 当前 bot 使用的 workspace / component。
- 当前 RootMessage 对应的 Session 和 backend thread 状态。
- binding 未完成 workspace 配置时的阻塞原因。

群公告状态条是独立 issue，但 issue 9 需要提供稳定的状态数据源，不能继续把所有 binding 状态拼在普通执行卡片里。

## 7. 后续建议实现顺序

### Phase A: BotProfile 继承

- 建立 bot 私聊 profile 的持久化模型。
- 固定 `profile -> binding -> session/thread` 的继承和覆盖边界。
- 明确 profile 更新后是否只影响新 binding / 新 thread。

### Phase B: 群内菜单重组

- 将菜单一级结构进一步整理为项目、当前 Bot、当前对话、工具、管理员工具。
- 给菜单注册项补 `scope`、`backend`、`requires`、`visibility` 和 `mutating` 元数据。
- 继续保持菜单能力必须有 slash command 或等价直接入口。

### Phase C: Binding 生命周期边界

- 固定 binding 删除、workspace 变更、bot 离群、frontend 重装后的行为。
- 补充旧 session 在 binding 变更后的 thread/workspace 复用策略。
- 增加多 frontend 同群、不同机器、不同 workspace 的集成测试。

### Phase D: 状态展示对接

- 与群公告状态条和多人输入队列 issue 对接。
- 提供稳定的群级 / binding 级状态数据源。

## 8. 验收标准

issue 9 完成条件：

- [x] 同群不同 RootMessage 产生独立 Session。
- [x] 同 RootMessage 的不同 bot 产生 frontend 隔离的 Session。
- [x] binding 不出现在 SessionKey 中。
- [x] 每个 bot 可以使用自己机器上的 workspace。
- [x] 新 bot 入群后可以通过群内 `/bind` 选择已有、新建或 clone workspace。
- [x] 未完成 workspace 配置时有明确可操作的提示。
- [x] binding 级 model / effort / service tier / sandbox / approval policy / multi-agent / Claude permission mode 参与运行时解析。
- [x] 群内 binding 配置命令有明确作用域；进入 binding mode 后，`/workspace`、`/model`、`/effort`、`/fast` 的相关配置入口写入当前 bot 的本地 binding。
- [x] 群内 menu 增加当前 Bot / Binding 入口，菜单卡片不提供 bot 选择列表。
- [x] 通过 `@BotB /menu` 或其他明确 mention 命令可以直接进入 BotB 的本地菜单和配置流程。
- [x] 当前新增本地 binding 能力可以从 `/menu` 进入，并保留 `/bind` slash command 入口。
- [x] binding 持久化、重启恢复、workspace 解析、路由和 primary 边界有测试；bot 离群清理作为后续运维/群成员事件 issue。
- [x] 不破坏现有 Codex app-server thread/turn/approval 状态机。

非本 issue 验收项：BotProfile 私聊配置继承、完整群级状态条、多人输入队列、bot 离群清理和跨实例协作协议。

## 9. 相关实现

- [Session key](../internal/app/appcore/session_key.go)
- [Agent binding state](../internal/state/store.go)
- [Agent binding app scope](../internal/app/appstate/agent_binding.go)
- [Group message routing](../internal/app/group_message_policy.go)
- [Feishu event routing](../internal/app/feishu_event_router.go)
- [Submission binding](../internal/app/submission/queue.go)
- [Workspace resolution](../internal/app/workspace_selection.go)
- [Binding effective config](../internal/app/binding_effective.go)
- [Binding service](../internal/app/binding_service.go)
- [Binding-scoped command wrappers](../internal/app/binding_scoped_commands.go)
- [Binding menu registration](../internal/app/feature_registry_bindings_binding.go)
- [Workspace/model/fast feature registration](../internal/app/feature_registry_bindings_thread_workspace.go)
- [Workspace card action routing](../internal/app/action_registry_workspace.go)
- [State tests](../internal/state/agent_binding_test.go)
- [Group routing tests](../internal/app/group_message_policy_test.go)
- [Workspace tests](../internal/app/binding_workspace_test.go)
- [Binding service tests](../internal/app/binding_service_test.go)
- [Codex app-server state-machine audit](codex-app-server-state-machine-audit.md)
