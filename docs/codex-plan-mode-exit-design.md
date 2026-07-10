# Codex Plan Mode 退出设计与实现计划

状态: 设计中

最后更新: 2026-05-13

## 目标

为 Codex backend 的 `/plan` 补齐“生成计划后如何退出 plan mode 并进入实现”的完整产品契约，并给出可直接落地的实现计划。

本设计只讨论 Codex 路径，不复用 Claude 的 plan mode 交互。

## 当前已实现部分

Feidex 当前已经接入了 Codex `/plan` 的入口半程:

- `/plan on` 会先要求 `cfg.Codex.ExperimentalAPI == true`。
- 开启 plan mode 时，不猜 preset，而是调用 `collaborationMode/list` 读取服务端提供的 collaboration mode 列表。
- 找到 `mode=plan` 的 preset 后，会为当前 thread 保存 `SessionCollaborationMode`。
- 后续该 thread 的 submission 在调用 `turn/start` 时，会自动带上 `params.collaborationMode`。
- `developer_instructions` 必须显式传 `null`，而不是空字符串或默认文案。

当前仍未补齐的是 plan mode 的“出口”:

- turn 完成后，没有按 Codex 标准给出“是否实现该计划”的选择。
- 还没有区分“在当前 thread 实现”与“清上下文后在新 thread 实现”。
- 还没有围绕队列、steer、pending form、plan item 流式增量等边界做完整状态收口。

## 协议语义澄清

### `item(type=plan)` 和 `turn/plan/updated` 不是一回事

- `item(type=plan)` 是 plan mode turn 的正式计划输出。
- `item/plan/delta` 是它的可选流式增量。
- `turn/plan/updated` 是 turn 级 checklist 更新，只表示步骤状态展示。
- `turn/plan/updated` 不是 plan mode，也不是 `item(type=plan)` 的前置、别名、降级版或等价物。

因此本设计里:

- “plan mode 结果”只认 `item(type=plan)` 这一条线。
- “执行中 checklist 展示”只认 `turn/plan/updated` 这一条线。
- 两者的 UI、状态、触发条件和后续动作必须彻底分开，不允许混用。

## 与 Codex 标准对齐的行为

Codex TUI 在 plan mode turn 完成后，会给用户一个确认选择:

- 标题: `Implement this plan?`
- 选项 1: `Yes, implement this plan`
- 选项 2: `Yes, clear context and implement`
- 选项 3: `No, stay in Plan mode`

Feidex 需要对齐的是这三个动作的语义，而不是必须复用 TUI 代码。

第一阶段建议直接复用这组英文标题和选项，降低语义漂移风险；后续如果要本地化文案，也必须保持完全相同的动作语义。

## 三个选项的产品语义

### 1. `Yes, implement this plan`

语义:

- 退出当前 active thread 的 plan mode。
- 在当前 thread 继续工作，不新开 thread。
- 自动提交一条实现指令: `Implement the plan.`

本地状态变化:

- 先把 `sess.ActiveThreadCollaborationMode` 写成 `mode=default`，让下一次同 thread 的 `turn/start` 显式覆盖服务端保存的 `mode=plan` 默认值。
- 这个 `mode=default` 必须同时恢复普通 model 和普通 reasoning effort；`turn/start.params.collaborationMode` 会覆盖顶层 model/effort，如果只传 default mode + model，后续 turn 会退回服务端默认推理强度。
- 再把实现 submission 加入当前 session 队列并启动。

### 2. `Yes, clear context and implement`

语义:

- 不 fork 当前 thread。
- 直接按 `/new` 语义创建一个新的 Codex thread。
- 把旧的 plan thread 留在历史里，用户之后如有需要可自行 resume。
- 在新 thread 里提交一条“带计划全文的 fresh-context 实现提示词”。

这里必须强调:

- 这条路径和 Codex 一样，是 fresh session / fresh thread 语义，不是 `thread/fork`。
- 旧 thread 是否之后被用户 resume，是线程历史问题，不是本次 handoff 的一部分。
- 当前范围只要求旧 thread 可被 resume 回来看历史；不要求自动恢复它之前的 plan mode 开关。

新 thread 的首条提示词应对齐 Codex:

```text
A previous agent produced the plan below to accomplish the user's task. Implement the plan in a fresh context. Treat the plan as the source of user intent, re-read files as needed, and carry the work through implementation and verification.
```

然后追加两行空行，再拼接 plan markdown。

### 3. `No, stay in Plan mode`

语义:

- 什么都不提交。
- 保持当前 thread 仍处于 plan mode。
- 用户可以继续追加规划、修订计划、或稍后再决定是否实现。

## 何时应该弹出退出确认

退出确认不是“每次 turn 完成都弹”，而是只在真正的 plan mode 结果完成后才弹。

### 必须同时满足的条件

- 当前 backend 是 Codex。
- 当前 turn 最终完成为可继续收口的终态，通常是 `completed`。
- 该 turn 里出现了真实的 `item(type=plan)` 输出。
- 当前计划文本非空。
- 当前 session 和 thread 已完成 turn 收尾，不再处于 active work / steer cleanup 中。
- 当前没有更高优先级的后续用户意图已经排队或开始执行。

### 明确不应触发的情况

- 只收到 `turn/plan/updated` checklist 更新，没有真实 plan item。
- 当前 turn 是普通 default mode 回答、review、compact、approval、或其他非 plan turn。
- 计划 turn 完成前后，用户已经排入新的 submission。
- 当前还有活跃 steer 或 turn cleanup 尚未结束。
- 当前已经出现更新的 turn，旧的 plan 结果已经过时。
- 当前已经存在同一个 plan turn 的退出确认 pending，不应重复发。

## 阻塞类型: 只在 terminal 边界当场判断

为了避免旧提示误弹，退出确认只在 `turn/completed` 收尾这一刻判断一次。

### 直接丢弃该 turn 的退出确认

以下情况说明用户已经继续往前走了，旧 plan 不应再回来打断:

- session 队列里已经有新的 submission。
- 新的 turn 已经开始。
- 用户主动发送了 follow-up 消息。
- 用户对后续流程做了新的 steer 或等价继续动作。
- thread 已切换到别的会话上下文。
- 当前还有任何 open pending request，包括 approval、permissions、elicitation、`tool/requestUserInput` 表单或其他同 session pending。
- 本地异步卡片动作尚未完成 patch，session 还没回到稳定空闲态。

这些情况一旦发生，该 plan turn 的退出确认应永久作废，不记录候选、不做补弹。

## `tool/requestUserInput` / 表单等待时如何处理

这类交互优先级高于 plan-mode 退出确认。

规则如下:

- 如果 turn 仍在等待 `tool/requestUserInput`，则绝不弹 plan-mode 退出确认。
- 用户应先完成表单输入，继续让该 turn 正常推进。
- 只有该 turn 真正走到 terminal completion 且当时没有其他 pending blocker，才允许出现退出确认。

如果是“不属于该 turn，但同 session 里还有其他 open pending request”，也直接不弹，不补发。

## `plan 生成之后又 steer 修改了` 如何处理

这一点必须按 turn 终态来定义，而不能按“第一次看到计划”定义。

规则:

- 只要 turn 还没完成，plan 文本都还可能被同一个 turn 的后续 steer 改写。
- 如果同一个 turn 内 plan 输出被修订，退出确认必须使用该 turn 最终版本的 plan markdown。
- 不允许在第一次看到 plan item 时立刻发退出确认；必须等到 turn 收口。

实现上，应该把“当前 turn 最终计划文本”作为 turn stream 的一部分来跟踪，而不是只看即时卡片。

## `Yes, clear context and implement` 为什么必须是 `/new`，不是 `fork`

Codex 的标准行为是“清空当前上下文，开一个新的 session/thread，然后把计划作为新的初始用户意图提交进去”。

这和 `fork` 的区别很明确:

- `/new` 会让新 thread 只看到明确传入的新提示词，不继承当前 thread 的上下文窗口。
- `fork` 仍然是从旧 thread 分叉，保留上文语义，这不符合 “clear context”。
- Codex TUI 对应的是 `ClearUiAndSubmitUserMessage`，其语义就是 start fresh session，而不是 fork。

所以 Feidex 的对齐策略应是:

- 新开 Codex thread，复用当前 workspace。
- 不走 `forkCodexActiveConversation`。
- 旧 plan thread 原样保留，用户之后如需回看，可手动 resume。

## Feidex 侧的实现约束

### 1. 这是一个本地 pending，不是 app-server server request

该退出确认来自 Feidex 自己的产品编排，不是 Codex server 主动发来的 request。

因此:

- 需要引入一个新的本地 pending kind，例如 `codex_exit_plan_mode`。
- 不应把它误建模为 `serverRequest/resolved` 驱动的请求。
- 不应复用 Claude 的 plan mode pending 实现。

### 2. 不能使用 `deliverPendingCard`

`deliverPendingCard(...)` 会把 submission 改成 waiting 状态。

这在这里是错误的，因为:

- 计划 turn 对应的 submission 此时已经完成。
- 退出确认只是一个“后续动作选择器”，不是原 submission 仍在等待 server response。

正确做法:

- 直接发送一个本地 reply card。
- 手工保存 `PendingRequest`。
- 不修改原 submission 的 terminal 状态。

### 3. 卡片按钮必须走快 ack + 异步 patch

三个按钮里，尤其是“清上下文后实现”，可能涉及:

- 启动新 thread。
- 写 session 状态。
- 入队并启动新 submission。

这些都不应该在同步 card callback 里阻塞执行。

因此必须遵守仓库约束:

- 同步 callback 立即 ack。
- 后台异步完成实际动作。
- 最后 patch 卡片为成功或失败结果。

现有 `completeAsyncRenderedCardAction(...)` 可作为这一层的实现模板。

## 推荐的状态模型

### Turn Stream 需要新增的 plan-mode 轨迹

当前 `turnstream.Stream` 只跟踪 checklist 风格的 `PendingPlan`，不足以支撑 plan-mode 退出确认。

建议增加:

- `SawPlanItem bool`
- `PlanItemID string`
- `PlanMarkdown string`
- `PlanCompleted bool`

语义:

- `PendingPlan` 继续只服务 `turn/plan/updated` checklist 展示。
- `PlanMarkdown` 只服务 `item(type=plan)` 的最终文本和退出确认。

### 不记录“待显示的退出确认候选”

退出确认没有延后补发机制。`turn/completed` 当下只会得到两个结果:

- 满足所有条件: 立即发出退出确认。
- 存在队列、active work、其他 pending 或 thread/session 已变化: 直接丢弃该 turn 的退出确认。

## 事件处理与数据流

### 1. 暂不接入 `item/plan/delta`

当前客户端显式 opt-out 了 `item/plan/delta`。

本阶段保持这个策略:

- 不从 `optOutNotificationMethods` 中移除 `item/plan/delta`。
- 不累积 plan delta。
- 只以 `item/completed(type=plan)` 作为 plan item 的最终收口点。

即:

- `completed` 负责最终 plan 文本确认。
- 退出确认仍以 turn terminal completion 为触发边界。

### 2. turn 完成时判断是否创建退出确认

在 turn lifecycle 的 terminal completion 收尾点执行:

1. flush 当前 turn stream
2. 拿到该 turn 的最终 plan-mode 结果
3. 判断是“直接丢弃”还是“立刻发卡”

### 3. 发卡方式

如果满足立即发卡:

- 发送一张新的 reply card
- 保存 `PendingRequest{Kind: "codex_exit_plan_mode"}`
- payload 至少包含 `planMarkdown`

卡片上需要三个动作:

- `codex_plan_mode.implement_current`
- `codex_plan_mode.implement_fresh`
- `codex_plan_mode.stay`

动作名可以微调，但应该保持 Codex 专属命名，不和 Claude 共用。

### 4. 用户选择后的动作

`implement_current`:

- 校验 pending 仍有效且 owner 匹配
- 将 `ActiveThreadCollaborationMode` 改为 `mode=default`
- 以当前 thread 新建 submission: `Implement the plan.`

`implement_fresh`:

- 校验 pending 仍有效且 owner 匹配
- 走 `/new` 等价路径创建新的 Codex thread
- 不走 fork
- 在新 thread 提交 clear-context prompt + plan markdown

`stay`:

- 仅关闭该 pending
- 维持当前 thread 的 plan mode 不变

### 5. 旧 pending 失效处理

如果用户在退出确认仍挂着时，又做了新的继续动作，则需要把旧确认标成过期或已取消，避免两个控制面并存。

至少要覆盖:

- 新 submission 入队
- thread 切换
- 显式 `/plan off`
- 新的 plan turn 结果覆盖旧 turn

## 旧 thread 恢复语义

本次设计只承诺以下行为:

- 选择 `Yes, clear context and implement` 后，旧 plan thread 仍作为一条历史 thread 保留。
- 用户之后可以自己 resume 回去查看原计划或继续对话。

本次设计不额外承诺:

- 自动为被 resume 的旧 thread 恢复 plan mode 开关。

如果未来产品需要“手动 resume 某条 Codex thread 时自动恢复该 thread 之前的 collaborationMode”，那是独立的数据建模改动，应另开设计，不应和本次退出确认耦合。

## 建议改动文件

协议与事件:

- `internal/codexrpc/client.go`
- `internal/app/backend/codex_event_router.go`
- `internal/app/codex_event_router.go`

turn stream / lifecycle:

- `internal/app/turnstream/service.go`
- `internal/app/turnlifecycle/service.go`

本地 pending 与卡片动作:

- `internal/app/action_registry_pending.go`
- `internal/app/serverrequest_bindings.go`
- `internal/app/card_action_async.go`
- `internal/app/planmode/exit.go` 和 root `internal/app/plan_mode_bindings.go`

thread / submission 调度:

- `internal/app/workspacecmd/thread_service.go`
- 当前 thread submission 入队相关服务

测试与文档:

- `internal/app/plan_mode_test.go`
- 新增 plan-mode exit 行为测试
- `docs/codex-app-server-state-machine-audit.md`

## 分阶段实现计划

### Phase 1: 补齐协议消费与 turn stream 状态

- 保持 `item/plan/delta` opt-out，不在本阶段接入流式 plan delta
- 在 turn stream 中区分 checklist plan 和 plan item plan
- 让 `FlushResult` 返回 plan-mode 结果

### Phase 2: 补齐退出确认卡片和 pending

- 新增 `codex_exit_plan_mode` 本地 pending
- 新增卡片渲染
- turn 完成后按规则发卡或直接丢弃

### Phase 3: 补齐三个动作

- 当前 thread 实现
- fresh-context 实现
- stay in plan mode

### Phase 4: 补齐失效机制

- follow-up 入队导致旧 pending 作废
- 线程切换或 `/plan off` 时清理陈旧确认

### Phase 5: 测试与协议文档同步

- turn 完成触发退出确认
- checklist-only turn 不触发
- queue / steer / pending form 阻塞场景
- `implement_current` 会先退出 plan mode 再提交
- `implement_fresh` 走 `/new` 语义，不走 fork
- 文档同步更新状态机审计，明确 `item(type=plan)` 和 `turn/plan/updated` 的边界

## 验收标准

- `/plan on` 后，真实 plan-mode turn 完成时，Feishu 能看到标准三选一确认。
- checklist-only 更新不会误触发确认。
- `Yes, implement this plan` 不会再次进入 plan mode。
- `Yes, clear context and implement` 会打开新的 Codex thread，而不是 fork 旧 thread。
- 旧 plan thread 仍可手动 resume。
- 队列中已有 follow-up、或旧计划已过时，不会再弹陈旧确认。
- `turn/completed` 当下有其他 open pending 时，不发退出确认，也不在 pending 清除后补发。
- 等待用户表单输入时，不会被 plan-mode 退出确认抢占。

## 非目标

- 不在本次实现里复用 Claude plan mode 代码或统一两套 backend 的 plan 交互。
- 不在本次实现里引入 `turn/plan/updated` 和 `item(type=plan)` 的统一抽象。
- 不在本次实现里接入 `item/plan/delta`。
- 不在本次实现里承诺“任意 thread resume 自动恢复之前的 collaborationMode”。
