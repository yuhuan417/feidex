# Codex App Server 状态机审计

审计时间: 2026-07-10

官方来源:
- `https://developers.openai.com/codex/app-server`

本地对照来源:
- `internal/codexrpc/client.go`
- `internal/app/codex_event_router.go`
- `internal/app/backend/codex_event_router.go`
- `internal/app/app.go`
- `internal/app/submission_queue.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/approval_summaries.go`
- `internal/app/serverrequest/approval.go`
- `internal/app/serverrequest/user_input.go`
- `internal/app/serverrequest/elicitation.go`
- `internal/app/serverrequest/adapter.go`
- `internal/app/tool_user_input_forms.go`
- `internal/app/submission/pending_forms.go`
- `internal/app/pendingforms/elicitation_form.go`
- `internal/app/approval/permission_summary.go`
- `internal/app/compact.go`
- `internal/app/goal.go`
- `internal/app/skillscmd/service.go`
- `internal/app/skills/skills.go`
- `internal/codexrpc/goal.go`
- `internal/codexrpc/skills.go`
- `tmp/appserver-schema/`

## 审计口径

- `严格遵循`: 请求/通知顺序、状态边界、最终确认边界都和官方文档一致，客户端不会提前推进本地状态。
- `兼容实现`: 核心交互节点按正确时序处理，最终结果完整；只是刻意忽略某些中间态、流式增量或冗余通知，不影响用户信息获取和交互成功。
- `部分遵循`: 主路径能跑通，但遗漏了官方要求的某些通知、状态边界或决策分支，存在“看起来能用，但不是严格按协议”的情况。
- `未遵循`: 当前实现会错过、误处理、提前结束或直接拒绝官方描述的流程。
- `未实现`: 官方文档里有该流程，但 Feidex 既没有发起该方法，也没有消费对应通知/请求。
- 本文只审计 app-server 的方法、通知、server request 及其生命周期，不单独审计 `thread/read` 这类结果 payload 的枚举字段。
- 因此像 `userMessage.content` 里的 `skill`、`mention`、`localImage` 这类输入项类型，属于历史/展示层兼容范围，不单列为 `SM-xx` 状态机。

## 术语澄清: `item/plan` vs `turn/plan/updated`

- `item(type=plan)` 和可选的 `item/plan/delta` 属于 item 生命周期，语义是 plan-mode turn 产出的计划文本。
- 在 Feidex 当前语境里，这条线对应 Codex `collaborationMode.mode=plan` 的 turn 输出，也就是 `/plan` 刚接入的 plan mode 结果展示。
- `turn/plan/updated` 是 turn 级 checklist 更新通知，payload 是 `[{step, status}]` 这样的步骤列表。
- `turn/plan/updated` 在 Feidex 里只被当成执行中 checklist 的展示来源，不代表 plan mode，也不是 `item(type=plan)` 的别名、前置事件、降级事件或等价事件。
- 两者没有协议上的从属关系；后文凡是提到 plan mode / plan item，都指 `item(type=plan)` 这条线，凡是提到 checklist 更新，都只指 `turn/plan/updated`。

## 状态机命名

后续讨论统一使用 `SM-xx` 代号引用状态机:

| 代号 | 名称 |
| --- | --- |
| `SM-01` | `InitializeHandshake` |
| `SM-02` | `ExperimentalApiNegotiation` |
| `SM-03` | `ThreadBootstrapAndLifecycle` |
| `SM-04` | `TurnLifecycleCore` |
| `SM-05` | `TurnSteerContinuation` |
| `SM-06` | `TurnInterruptCompletion` |
| `SM-07` | `TurnErrorFailureCompletion` |
| `SM-08` | `ThreadCompactionLifecycle` |
| `SM-09` | `CommandApproval` |
| `SM-10` | `FileApproval` |
| `SM-11` | `ToolRequestUserInput` |
| `SM-12` | `AppToolApprovalViaUserInput` |
| `SM-13` | `DynamicToolCall` |
| `SM-14` | `ReviewLifecycle` |
| `SM-15` | `(merged into SM-03)` |
| `SM-16` | `ThreadShellCommand` |
| `SM-17` | `CommandExecSession` |
| `SM-18` | `WindowsSandboxSetup` |
| `SM-19` | `FuzzyFileSearchSession` |
| `SM-20` | `AccountAuthLifecycle` |
| `SM-21` | `McpOAuthLoginLifecycle` |
| `SM-22` | `PermissionsApproval` |
| `SM-23` | `McpElicitationRequest` |
| `SM-24` | `SkillsCatalogLifecycle` |
| `SM-25` | `ThreadGoalLifecycle` |

## 状态机到测试映射

下表只列当前产品里最关键、最容易因为协议漂移而回归的状态机 guard。目标不是追求行覆盖率，而是把“协议边界一旦变了就会出事故”的路径固定成自动化测试。

| 代号 | 风险点 | 当前 guard |
| --- | --- | --- |
| `SM-01` | 每条连接只做一次 `initialize -> initialized` 握手 | `internal/codexrpc/client_test.go`、`internal/codexrpc/integration_live_test.go` |
| `SM-02` | experimental API / opt-out 协商不漂移 | `internal/codexrpc/client_test.go`、`internal/codexrpc/integration_live_test.go` |
| `SM-03` | `thread/start` 后 thread 元数据与本地 session 绑定不乱 | `internal/codexrpc/integration_live_test.go`、`internal/app/critical_paths_test.go`、`internal/app/critical_paths_more_test.go` |
| `SM-04` | `turn/start`、`turn/started`、`turn/completed` 与队列恢复不乱序 | `internal/app/critical_paths_test.go`、`internal/app/critical_paths_more_test.go`、`internal/app/protocol_business_logic_test.go`、`internal/app/codex_turn_recovery_test.go`、`internal/app/quiet_working_card_test.go`、`internal/codexrpc/integration_live_state_machine_test.go` |
| `SM-05` | `turn/steer` 必须绑定 `expectedTurnId`，reply follow-up 不能误开新 turn | `internal/app/app_more_test.go`、`internal/app/steer_more_test.go`、`internal/codexrpc/integration_live_state_machine_test.go` |
| `SM-06` | `turn/interrupt` 只请求中断，真正收口仍等 `turn/completed(interrupted)` | `internal/app/state_machine_contracts_test.go`、`internal/app/notifications_branches_more_test.go` |
| `SM-07` | `error` 只记录失败上下文，不得早于 `turn/completed(failed)` 清 session | `internal/app/state_machine_contracts_test.go`、`internal/app/app_more_test.go` |
| `SM-08` | compact 走 `contextCompaction` item 生命周期，不退回 deprecated 路径 | `internal/app/compact_more_test.go`、`internal/app/notifications_branches_more_test.go` |
| `SM-09` | command approval 必须等待 `serverRequest/resolved` 才恢复 submission | `internal/app/critical_paths_test.go`、`internal/app/item_started_server_request_test.go`、`internal/app/protocol_business_logic_test.go`、`internal/app/quiet_working_card_test.go`、`internal/codexrpc/integration_live_state_machine_test.go` |
| `SM-10` | file approval 必须把 started item 上下文和 request payload 合并 | `internal/app/item_started_server_request_test.go`、`internal/app/app_more_test.go`、`internal/app/protocol_business_logic_test.go`、`internal/codexrpc/integration_live_state_machine_test.go` |
| `SM-11` | tool user input 的 reply / resolve / resume 边界不能错 | `internal/app/critical_paths_more_test.go` |
| `SM-13` | dynamic tool call 当前必须显式拒绝，不能半接入半放行 | `internal/app/state_machine_contracts_test.go`、`internal/app/app_more_test.go` |
| `SM-14` | `review/start` payload、review item 生命周期、final 渲染、持久化历史 | `internal/app/review_critical_test.go`、`internal/app/review_test.go`、`internal/app/protocol_business_logic_test.go`、`internal/codexrpc/integration_live_review_test.go` |
| `SM-22` | permissions approval 的 payload、reply、resolved 恢复契约 | `internal/app/state_machine_contracts_test.go`、`internal/app/notifications_branches_more_test.go`、`internal/app/app_more_test.go` |
| `SM-23` | MCP elicitation 的 url/form pending、reply、resolved 恢复契约 | `internal/app/state_machine_contracts_test.go`、`internal/app/notifications_branches_more_test.go`、`internal/app/app_more_test.go`、`internal/app/server_request_reply_error_test.go` |
| `SM-24` | skills/list payload、forceReload、pending/explicit skill input resolution | `internal/app/skills_test.go`、`internal/codexrpc/skills_test.go` |
| `SM-25` | thread goal 的 set/get/clear 请求、goal 通知缓存，以及 active goal 产生的 orphan turn 绑定 | `internal/app/goal_test.go`、`internal/codexrpc/goal_test.go` |

## 逐项审计

### SM-01 `InitializeHandshake`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原文: `Initialize once per connection`
  - 官方页面原文: `Clients must send a single initialize request per transport connection.`
  - 协议节点: `initialize -> initialized`
  - 来源: OpenAI 官方页面 `Initialization`
- 我们当前实现:
  - `internal/codexrpc/client.go` 在 transport 启动后立即发送 `initialize`，收到响应后立刻发送 `initialized`。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-02 `ExperimentalApiNegotiation`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原文: `experimentalApi?: string`
  - 官方页面原文: `Optional experimental API version used to opt in to experimental features. Omitting this signals stable-only behavior.`
  - 官方页面原文: `optOutNotificationMethods?: string[]`
  - 协议节点: `initialize.params.capabilities.experimentalApi`、`initialize.params.capabilities.optOutNotificationMethods`
  - 来源: OpenAI 官方页面 `Initialization`
- 我们当前实现:
  - `internal/codexrpc/client.go` 会在 `initialize` 的 `capabilities` 中声明 `experimentalApi` 与 `optOutNotificationMethods`。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-03 `ThreadBootstrapAndLifecycle`

- 结论: `兼容实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `thread/start` starts a new thread.
  - 官方页面原始协议名与约束: `thread/resume` resumes an existing thread.
  - 官方页面原始协议名与约束: `thread/fork` forks an existing thread into a new thread.
  - 官方页面原始协议名与约束: thread 启动后还会继续收到 `thread/started`、`thread/status/changed`、`thread/archived`、`thread/unarchived`、`thread/closed` 等生命周期通知。
  - 协议节点: `thread/start|thread/resume|thread/fork -> thread/started -> thread lifecycle notifications...`
  - 来源: OpenAI 官方页面 `Lifecycle overview`, `Thread methods`, `Notifications`
- 我们当前实现:
  - `internal/app/submission_queue.go` 发 `thread/start`。
  - `internal/app/thread_feature_actions.go` 发 `thread/resume`。
  - `internal/app/fork.go` 发 `thread/fork`。
  - 上述三条主链路都直接使用 RPC response 中返回的 thread 信息更新本地 session。
  - `internal/app/codex_event_router.go` 没有 `thread/started`、`thread/archived`、`thread/unarchived`、`thread/closed`、`thread/status/changed` 的处理分支。
- 差异点:
  - thread bootstrap 主链路可用，本地 thread 上下文由请求响应直接驱动，而不是等待后续 `thread/started` 通知。
  - thread 生命周期通知整体未接入，但当前场景也不要求做跨客户端 thread 状态同步。
- 修改建议:
  - 保持现状即可；仅当后续产品明确需要 thread 级状态同步或外部 lifecycle 观察时，再补 `thread/started` 与其他 thread 生命周期通知。

### SM-04 `TurnLifecycleCore`

- 结论: `兼容实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `turn/start` starts a turn on a thread.
  - 官方页面原始协议名与约束: `turn/started` announces the active turn.
  - 官方页面原始协议名与约束: `item/started` begins an item lifecycle.
  - 官方页面原始协议名与约束: `item/.../delta` streams incremental item output.
  - 官方页面原始协议名与约束: `item/completed` finishes the item.
  - 官方页面原始协议名与约束: `turn/completed` finishes the turn.
  - 协议节点: `turn/start -> turn/started -> item/started -> item/.../delta* -> item/completed -> turn/completed`
  - 来源: OpenAI 官方页面 `Lifecycle overview`, `Turn methods`, `Notifications`
- 我们当前实现:
  - `internal/app/app.go` 发送 `turn/start`。
  - `internal/app/submission_queue.go` 和 `internal/app/turn_lifecycle.go` 会处理返回的 `turn.id` 以及 `turn/started`。
  - `internal/app/codex_event_router.go` 现在同时消费 `item/started` 与 `item/completed`。
  - `internal/app/backend/codex_event_router.go` 额外消费 `item/mcpToolCall/progress`，并把它视为已 started item 的 in-flight 更新，而不是独立终态。
  - `internal/app/turn_item_state.go` 为每个 `turnId + itemId` 维护 started/completed 快照，并在 completed 时合并成最终 item 载荷。
  - `internal/app/turnitem/payload.go` 会把 `item(type=plan)` 当成普通 completed item 渲染成计划文本；这条线属于 plan mode item 生命周期。
  - `internal/app/codex_event_router.go` 消费 `turn/started` 和 `turn/completed`。
  - `internal/app/codex_event_router.go` 单独消费 `turn/plan/updated`，并把 `[{step,status}]` 转成 checklist markdown；这条线只是执行中 checklist 展示，不属于 `item(type=plan)` 生命周期。
  - `internal/app/turnitem/payload.go` 按 completed item 渲染最终内容。
  - `internal/app/plan_mode.go` 会在 `/plan` 开启时为 `collaborationMode.mode=plan` 明确带上 model；reasoning effort 优先取本地 `codex.plan_reasoning_effort`，否则透传 app-server 的 plan preset，若 preset 未提供则保持为空。
  - `/plan off` 或 plan-exit 的当前 thread 实现路径会把本地 mode 写回 `collaborationMode.mode=default`，并带上普通 `codex.reasoning_effort`；因为 `collaborationMode` 会覆盖 `turn/start` 顶层 model/effort，default mode 不能丢掉已配置的普通推理强度。
  - `internal/app/turnstream/service.go` / `internal/app/server_request_delivery_scaffold.go` 维护 Quiet Mode `工作中` 卡的复用边界: 只有只包含 reasoning placeholder (`思考中...`) 的 `工作中` 卡可以被第一条实质 turn 内容复用；一旦 `工作中` 卡已经包含 command/file/search/tool progress，它自身就是实质内容，不得再被审批、final 或 terminal output patch 覆盖。
  - 对同一 turn，approval、tool user input、MCP elicitation、agent message、plan、final output、terminal status 都属于实质内容。若 approval 等 server request 复用了 reasoning-only `工作中` 卡，后续 final output 必须新发回复卡，不能继续 patch 这张已变成审批卡的消息。
  - 当本地已经看到了 final output，但后续缺失 `turn/completed`、导致 session 卡在 in-flight 时，`internal/app/codex_turn_recovery.go` 会在下一次 `enqueue` 或显式 `/stop` 时，通过 `thread/read(includeTurns=true)` 对账该 `turnId` 的服务端终态；只有确认 turn 已进入 `completed|failed|interrupted` 后，才复用现有 `finishTurn` 收口本地状态。
  - `internal/codexrpc/client.go` 通过 `optOutNotificationMethods` 明确退订当前不接入的 item delta 通知。
- 差异点:
  - `item/agentMessage/delta`、`item/plan/delta`、`item/commandExecution/outputDelta`、`item/fileChange/outputDelta` 等流式通知被明确视为非目标能力，并通过 opt-out 退订。
  - `item/started` 已经接入，因此 tool approval / file change 这类需要前置 item 上下文的流程，已经不再只依赖 completed item。
  - `item/mcpToolCall/progress` 已接入，但只用于更新 started MCP item 的中间展示；真正收口仍然只认对应 `item/completed`。
  - 当前产品以 started/completed 为唯一 item 消费边界，因此 `item(type=plan)` 只消费最终 completed item，不消费 `item/plan/delta`。
  - `turn/plan/updated` 仍然保留，但它只是 checklist 展示通道；不要把它和 `/plan` collaboration mode 或 `item(type=plan)` 混为一谈。
  - 主协议边界仍然是 `turn/completed`；`thread/read` 对账只用于 missed notification 后的本地恢复，不把 final item 本身当作终态。
  - Quiet Mode 的 `工作中` 卡复用是展示层优化，不改变 turn/item/server request 的协议顺序；不得为了减少消息数而把后发生的 final output patch 到审批等实质内容之前的消息位置。
- 修改建议:
  - 保持现状即可；如果后续仍有其他明确不用消费的流式通知，也可继续加入 opt-out。

### SM-05 `TurnSteerContinuation`

- 结论: `兼容实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `turn/steer` steers the currently running turn.
  - 官方页面原始协议名与约束: `expectedTurnId` is required to bind the steer request to the active turn.
  - 官方页面原始协议名与约束: steer 继续在同一个 turn 内推进，不会新开 turn。
  - 协议节点: `turn/steer(expectedTurnId=...) -> same turn lifecycle`
  - 来源: OpenAI 官方页面 `Turn methods`
- 我们当前实现:
  - `internal/app/replycontinuation_bindings.go` 和 `internal/app/convbackend/helpers.go` 都会在 reply steer 路径带 `expectedTurnId`。
  - steer 后续沿用 started/completed 两阶段处理，流式 delta 不接入，并依赖 `internal/codexrpc/client.go` 中的 opt-out 退订相关通知。
- 差异点:
  - 请求形状正确。
  - 当前不展示 steer 期间的流式中间态，但这部分已明确不纳入实现范围。
- 修改建议:
  - 保持现状即可。

### SM-06 `TurnInterruptCompletion`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `turn/interrupt` interrupts the current turn.
  - 官方页面原始协议名与约束: interrupt 请求会立即返回；最终完成仍通过 `turn/completed` 观察。
  - 官方页面原始协议名与约束: 终态应为 `interrupted`。
  - 协议节点: `turn/interrupt -> turn/completed(status=interrupted)`
  - 来源: OpenAI 官方页面 `Turn methods`, `Notifications`
- 我们当前实现:
  - `internal/app/threadmenu/service.go` / `internal/app/backend/actions.go` 发送 `turn/interrupt` 或 backend-specific interrupt。
  - `internal/app/turn_lifecycle.go` 在 `turn/completed` 时把 `interrupted` 作为最终状态写回。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-07 `TurnErrorFailureCompletion`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `error` 用于报告 turn 失败过程中的错误。
  - 官方页面原始协议名与约束: `turn/completed` 仍然是最终 turn 生命周期收口点。
  - 官方页面原始协议名与约束: 终态应为 `failed`。
  - 协议节点: `error -> turn/completed(status=failed)`
  - 来源: OpenAI 官方页面 `Notifications`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 记录 `error`。
  - `internal/app/turn_lifecycle.go` 最终仍在 `turn/completed` 处 finalize。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-08 `ThreadCompactionLifecycle`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `thread/compact/start` starts compaction for a thread.
  - 官方页面原始协议名与约束: `contextCompaction` 被建模为 item，应该走标准 item 生命周期。
  - 官方页面原始协议名与约束: `thread/compacted` 已经 deprecated。
  - 协议节点: `thread/compact/start -> turn/started -> item/started(type=contextCompaction) -> item/completed -> turn/completed`
  - 来源: OpenAI 官方页面 `Thread methods`, `Notifications`
- 我们当前实现:
  - `internal/app/compact.go` 发送 `thread/compact/start`。
  - `internal/app/turn_lifecycle.go` / `internal/app/compact.go` 用 `turn/started` 和 `item/started(type=contextCompaction)` 绑定 standalone compact turn。
  - `internal/app/turn_stream.go` / `internal/app/compact.go` 用 `item/completed(type=contextCompaction)` 收口 standalone compact turn，后续 `turn/completed` 只做兜底。
- 差异点:
  - 无。主流程已经按文档迁移到 `contextCompaction` item 生命周期，且不再处理 deprecated `thread/compacted`。
- 修改建议:
  - 无需修改。

### SM-09 `CommandApproval`

- 结论: `兼容实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `item/started` emits a `commandExecution` item.
  - 官方页面原始协议名与约束: `item/commandExecution/requestApproval` asks the client for approval.
  - 官方页面原始协议名与约束: client 回复后，server 会发 `serverRequest/resolved`。
  - 官方页面原始协议名与约束: 最终由 `item/completed` 收口。
  - 协议节点: `item/started(commandExecution) -> item/commandExecution/requestApproval -> client decision -> serverRequest/resolved -> item/completed`
  - 来源: OpenAI 官方页面 `Tool approvals and requests`，以及 `tmp/appserver-schema/ServerRequest.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 会先接 `item/started`，再处理 `item/commandExecution/requestApproval`。
  - `internal/app/turn_item_state.go` 会把 started item 和 request payload 合并，审批卡片不再丢失 command item 上的上下文。
  - `internal/app/serverrequest/approval.go` 已支持 `accept`、`acceptForSession`、`decline`、`cancel` 四类 decision，并且只在 reply 成功后把本地请求推进到 `replied`。
  - `internal/app/server_request_state.go` 把 `serverRequest/resolved` 作为唯一 `resolved` 边界，并在同 turn 其他 open request 清空后才恢复 submission。
  - `internal/app/server_request_delivery_scaffold.go` 会优先把审批卡投递到同一 Feishu 回复上下文；如果当前 turn 只有 reasoning-only `工作中` 卡，审批卡可以 patch 复用该占位卡。复用后该消息已经是实质审批内容，后续 final output 不能再 patch 它。
  - `internal/app/codex_event_router.go` 仍会在最终 `item/completed` 时收口 item。
- 差异点:
  - command approval 的生命周期边界已经对齐。
  - schema 里虽然还有 `acceptWithExecpolicyAmendment`、`applyNetworkPolicyAmendment` 两类 amendment decision，但当前产品已明确不做这类 persistent amendment，因此不视为实现缺口。
- 2026-04-17 真实 Codex probe 补充:
  - 手动运行 `TestLiveCodexCommandApprovalLifecycleOnTinyRepo`，使用 tiny fixture、`approvalPolicy=on-request`、`sandbox=read-only`，并强制 prompt 要求走内建 command approval request 机制。
  - 当前稳定有效的 probe 形状不是常见系统命令，而是 tiny fixture 里自带的随机前缀本地脚本，例如 `./zz_feidex_probe_<nonce>.sh`。
  - 2026-04-17 同日连续 3 次真实运行里，这种随机前缀脚本都成功触发了 `item/commandExecution/requestApproval -> serverRequest/resolved -> item/completed(commandExecution) -> turn/completed`。
  - `thread/read(includeTurns=true)` 也同时保留了 `commandExecution` item、脚本命令文本，以及最终 `agentMessage` 对脚本 stdout 的回显。
  - 因此前述 app-layer contract test 与 current live Codex probe 现在已经能共同 guard `SM-09`。
  - 需要注意的是，同日较早的 probe 若使用 `/bin/bash -lc 'cat /proc/sys/kernel/random/uuid'` 这类常见 shell 包装命令，则曾观测到 Codex 直接执行 `commandExecution` 而不发协议级 approval request，甚至可能退化为普通 `agentMessage` 文本询问。说明 command approval 的真实触发与命令形状强相关，测试 fixture 必须保持“随机前缀本地脚本”这种非 trusted-looking 形态。
- 修改建议:
  - 保持现状即可；仅当后续产品重新需要 persistent exec/network policy amendment 时，再把对应 decision 与 UI 一起补回。
  - 不要把 live command approval probe 改回常见 shell/system command；那会重新引入 false negative。

### SM-10 `FileApproval`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `item/started` emits a `fileChange` item.
  - 官方页面原始协议名与约束: `item/fileChange/requestApproval` asks the client for approval.
  - schema 原始定义: `item/fileChange/requestApproval` 包含 `itemId`、`threadId`、`turnId`，并可带 `reason` 与 `grantRoot`。
  - 官方页面原始协议名与约束: client 回复后，server 会发 `serverRequest/resolved`。
  - 官方页面原始协议名与约束: 最终由 `item/completed` 收口。
  - 协议节点: `item/started(fileChange) -> item/fileChange/requestApproval -> client decision -> serverRequest/resolved -> item/completed`
  - 来源: OpenAI 官方页面 `Tool approvals and requests`，以及 `tmp/appserver-schema/ServerRequest.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 会先接 `item/started(fileChange)`，再处理 `item/fileChange/requestApproval`。
  - `internal/app/turn_item_state.go` 会把 started item 上的 `changes` 合并进审批请求，文件审批卡片可从 started item 补齐缺失文件列表。
  - `internal/app/approval/summary.go` 会显式渲染请求里的 `grantRoot`，避免文件列表存在时该字段被摘要逻辑吞掉。
  - `internal/app/serverrequest/approval.go` 和 `internal/app/server_request_state.go` 已支持 `accept`、`acceptForSession`、`decline`、`cancel` 四类 decision，并改成 `pending -> replied -> resolved`，等待 `serverRequest/resolved` 再最终收口。
  - `internal/app/codex_event_router.go` 仍会在最终 `item/completed` 时收口 item。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-11 `ToolRequestUserInput`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原文: `EXPERIMENTAL - Request input from the user for a tool call.`
  - 官方页面原文: `tool/requestUserInput - prompt the user with 1-3 short questions for a tool call`
  - 协议节点: `item/tool/requestUserInput -> client response -> serverRequest/resolved`
  - 来源: OpenAI 官方页面 `API overview`, `Tool approvals and requests`，以及 `tmp/appserver-schema/ServerRequest.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 收到 request 后发送 UI。
  - `internal/app/serverrequest/user_input.go`、`internal/app/tool_user_input_forms.go`、`internal/app/submission/pending_forms.go` 现在只在 reply 成功后把请求推进到 `replied`。
  - `internal/app/server_request_state.go` 统一把 `serverRequest/resolved` 当作唯一 `resolved` 边界，并负责恢复 submission。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-12 `AppToolApprovalViaUserInput`

- 结论: `兼容实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: 有副作用的 app tool 可能通过 `tool/requestUserInput` 要求用户 Accept / Decline / Cancel。
  - 官方页面原始协议名与约束: 如果用户 decline 或 cancel，关联的 `mcpToolCall` 会以 error 完成。
  - 协议节点: `mcpToolCall -> item/tool/requestUserInput -> accept|decline|cancel -> mcpToolCall completion`
  - 来源: OpenAI 官方页面 `Tool approvals and requests`
- 我们当前实现:
  - 走通用 `item/tool/requestUserInput` 分支，没有单独建模 app approval 状态机。
- 差异点:
  - UI 层能问能答。
  - 没有把它当成 `mcpToolCall` 生命周期的一部分单独建模，但当前产品不要求单独拆分这层状态机。
- 修改建议:
  - 保持现状即可。

### SM-13 `DynamicToolCall`

- 结论: `暂不实现（experimental）`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: server 会先发 `item/started(dynamicToolCall)`。
  - 官方页面原始协议名与约束: 然后发 `item/tool/call` 给客户端执行。
  - 官方页面原始协议名与约束: client 回结果后，由 `item/completed` 收口。
  - 协议节点: `item/started(dynamicToolCall) -> item/tool/call -> client result -> item/completed`
  - 来源: OpenAI 官方页面 `Tool approvals and requests`，以及 `tmp/appserver-schema/ServerRequest.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 没有 `item/tool/call` 分支，默认直接 `ReplyError(... unsupported server request)`。
- 差异点:
  - 当前将该状态机视为 experimental 范围外能力，不纳入实现范围。
  - 因此 `item/tool/call` 仍会被拒绝，`item/started(dynamicToolCall)` 也没有单独接入层。
- 修改建议:
  - 暂不实现；仅当后续产品明确决定支持 experimental dynamic tools 时，再补齐整条生命周期。

### SM-14 `ReviewLifecycle`

- 结论: `部分遵循`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `review/start` starts a review.
  - 官方页面原始协议名与约束: detached review 会先新建 thread，并通过 `thread/started` 报告。
  - 官方页面原始协议名与约束: review mode 通过 `enteredReviewMode` / `exitedReviewMode` item 表示。
  - 协议节点: `review/start -> thread/started? -> turn/started -> item/started(enteredReviewMode) -> ... -> item/completed(exitedReviewMode) -> turn/completed`
  - 来源: OpenAI 官方页面 `Turn methods`, `Notifications`
- 我们当前实现:
  - `internal/app/reviewcmd/service.go` / `internal/app/review_bindings.go` 已调用 `review/start`，并固定使用 `delivery = inline`。
  - `internal/app/turnitem/payload.go` 已消费 `enteredReviewMode` / `exitedReviewMode`；其中 `exitedReviewMode.review` 会被统一走最终答复渲染路径。
  - `internal/app/turn_stream.go` 在收到 review final 后会抑制 trailing `agentMessage`，避免 review 结果重复投递。
  - `internal/app/review_critical_test.go` 已覆盖 review target 解析、`review/start` payload、selector payload 更新。
  - `internal/app/review_test.go` 已覆盖 `exitedReviewMode` 最终渲染，以及 `review/start` response turn id 与后续 `turn/started` turn id 不一致时，客户端仍保持 response turn id 绑定。
  - `internal/codexrpc/integration_live_review_test.go` 增加手动 live inline review 测试，使用 tiny git fixture 验证 `review/start -> turn/started -> item/started(enteredReviewMode) -> item/completed(enteredReviewMode) -> item/completed(exitedReviewMode) -> turn/completed -> thread/read`。
- 2026-04-16 detached review 实测补充:
  - 测试方式: 连接本地启动的 `codex app-server`，对 `review/start` 进行真实协议探测；分别验证空 thread 与已 materialize thread。
  - 空 thread 上直接调用 `review/start` 且 `delivery = detached` 时，服务端返回 `error creating detached review thread: No such file or directory (os error 2)`。
  - 同场实测可见，空 thread 虽然在 `thread/start` 响应里已有 `thread.path`，但对应 rollout 文件实际尚未落盘；因此 detached review 当前可视为依赖 source thread 先完成至少一个 turn，使 rollout 文件 materialize。
  - source thread 完成一个普通 turn 后，再调用 `review/start` 且 `delivery = detached`，detached review 可成功启动。
  - 成功时观测到的新 thread 生命周期为:
    - `review/start(detached)` response 返回 `reviewThreadId`，同时返回一个 `turn.id = A`
    - `thread/started(reviewThreadId)`
    - `item/started(enteredReviewMode, turnId = A)`
    - `item/completed(enteredReviewMode, turnId = A)`
    - `thread/status/changed(active)`，作用于 review thread
    - `turn/started(turnId = B)`，其中 `B != A`
    - review 过程中的 `userMessage`、`reasoning`、`exitedReviewMode`、最终 `agentMessage`、`turn/completed` 仍都挂在 `turnId = A` 上
    - `thread/status/changed(idle)`，作用于 review thread
  - 因此当前实现里，`review/start` response 返回的 `turn.id` 与后续 `turn/started.turn.id` 可能不一致；客户端若要跟踪 detached review 完成边界，不能只依赖 `turn/started` 的 id。
  - review 完成后，没有观测到任何“切回 source thread”的通知；source thread 在 detached review 启动后到 review thread 完成后的静默窗口内，也没有收到新的 `thread/*`、`turn/*`、`item/*` 通知。
  - 进一步对 `thread/read(includeTurns = true)` 的实测表明，最终持久化历史并不是“source thread 与 review thread 各自都含有 review 内容”:
    - source thread 只保留自身原始 turn，不会写入 detached review 的任何 item 或 turn。
    - review thread 会带着一份 fork 时刻的 source history 副本。
    - review thread 的持久化 turns 中，除 fork 过来的 source turn 外，还会新增 review 相关 turns。
    - `enteredReviewMode` 在持久化历史里可能单独占一个 turn。
    - 通知流中的 `review/start` response.turn.id 与 `item(exitedReviewMode)`/`turn/completed` 使用的 turn id，以及最终 `thread/read` 里真实落盘的 turn ids，三者可能继续不一致。
    - 在 2026-04-16 的一次真实探测中，`review/start` response.turn.id 与 `exitedReviewMode` 通知里的 `turnId` 均未出现在最终 `thread/read` 的 turns 列表中；真正持久化的是 `turn/started` 通知里的另一个 turn id。
  - 结论上，若产品要在 UI 上“review 完成后切回旧 thread”，必须由客户端自行记住 source thread 并主动恢复焦点，不能依赖 app-server 发一个显式 switch-back 事件。
  - 同时，如果 UI 切回旧 thread，也不能假设 review 结果已经进入旧 thread 的模型上下文；更合理的做法是把 detached review 结果作为 source thread 的本地 sidecar / backlink 展示，而不是伪造成 source thread 的真实历史。
- 2026-04-16 当前讨论沉淀:
  - detached review 的真实语义更接近“fork 当前 thread 后在 fork 上做 review”，而不是“开一个 fresh thread 做 review，再把结果带回原 thread”。
  - 因此 detached review 主要解决的是“review 污染当前 thread 上下文”的风险，而不是“review 结果自动回灌原 thread”的需求。
  - “fresh review thread + review 完成后把结果自然带回原 thread”是另一种产品形态，不应与当前 app-server 的 detached review 语义混为一谈。
  - 当前产品判断: Feidex 先只实现 inline review。
  - inline review 不增加 review 专属的 active-thread 兜底或切换逻辑，沿用普通 submission 的通用 thread 生命周期处理。
  - 如果使用者担心 review 污染当前 thread，可由使用者先自行选择 `/new` 或 `/fork`，再在目标 thread 中执行 inline review。
  - 因此当前不把 detached review 作为 Feidex 的首期实现目标，也不为 detached review 设计结果回灌 source thread 的额外机制。
- 2026-04-17 inline review 实测补充:
  - 真实运行 `review/start(delivery=inline, target=uncommittedChanges)` 时，观测到 `item/started(enteredReviewMode)` / `item/completed(enteredReviewMode)` 可能先于 `turn/started`。
  - 同次实测里，`turn/started.turn.id` 可能不同于 `review/start` response.turn.id。
  - 但 `item/completed(exitedReviewMode)` 与 `turn/completed` 仍绑定在 `review/start response.turn.id` 上。
  - 因此 inline review 也不能把 `turn/started` 视为 review lifecycle 的唯一 authoritative turn id；当前实现继续以 `review/start response.turn.id` 作为 review submission 的主绑定是必要的。
- 差异点:
  - `SM-14` 的 inline 子集已经接入，并且有 contract test + manual live test 双重 guard。
  - detached review 仍未接入 `thread/started` / `thread/status/changed` / source-review 双线程关系处理，因此 `SM-14` 整体仍只能算 `部分遵循`，不能算 `严格遵循` 或 `兼容实现`。
- 修改建议:
  - 保持 inline review 作为当前受测主路径，并继续使用 tiny fixture 的手动 live test 监控真实协议漂移。
  - 若后续决定支持 detached review，必须把 source thread 与 review thread 的双线程关系单独建模，并补独立的状态机测试；不要复用 inline review 的假设。

### SM-15 `ThreadLifecycleNotifications`

- 已并入 `SM-03 ThreadBootstrapAndLifecycle` 统一评估，不再单列。

### SM-16 `ThreadShellCommand`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `thread/shellCommand` 在 thread 上下文里执行 shell command。
  - 官方页面原始协议名与约束: 请求立即返回，后续工作通过标准 turn/item 通知推进。
  - 协议节点: `thread/shellCommand -> turn/started -> item/... -> turn/completed`
  - 来源: OpenAI 官方页面 `Thread methods`，以及 `tmp/appserver-schema/codex_app_server_protocol.v2.schemas.json`
- 我们当前实现:
  - 没有对应 client 调用。
- 差异点:
  - 该能力与当前场景无关，因此整条链路不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要暴露 thread 级 shell command 时再实现。

### SM-17 `CommandExecSession`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `command/exec` 是独立于 thread 的一次性命令执行。
  - 官方页面原始协议名与约束: `command/exec/outputDelta` 用于流式 stdout/stderr。
  - 官方页面原始协议名与约束: PTY 还会衍生 `command/exec/write`、`command/exec/resize`、`command/exec/terminate`。
  - 协议节点: `command/exec -> command/exec/outputDelta* -> command/exec response`
  - 来源: OpenAI 官方页面 `Command methods`, `Notifications`，以及 `tmp/appserver-schema/codex_app_server_protocol.v2.schemas.json`
- 我们当前实现:
  - 没有对应 client 调用。
  - 没有 `command/exec/outputDelta` 处理。
- 差异点:
  - 该能力与当前场景无关，因此整条链路不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要独立命令执行 session 时，再实现 session 与 output delta 处理。

### SM-18 `WindowsSandboxSetup`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `windowsSandbox/setupStart`
  - 官方页面原始协议名与约束: `windowsSandbox/setupCompleted`
  - 协议节点: `windowsSandbox/setupStart -> windowsSandbox/setupCompleted`
  - 来源: OpenAI 官方页面 `Windows-specific methods`，以及 `tmp/appserver-schema/codex_app_server_protocol.v2.schemas.json`
- 我们当前实现:
  - 没有对应调用或通知处理。
- 差异点:
  - 该能力与当前场景无关，因此整条链路不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要 Windows sandbox 支持时，再补。

### SM-19 `FuzzyFileSearchSession`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `fuzzyFileSearch`
  - 官方页面原始协议名与约束: `fuzzyFileSearch/sessionUpdated`
  - 官方页面原始协议名与约束: `fuzzyFileSearch/sessionCompleted`
  - schema 原始定义: `FuzzyFileSearchParams` requires `query` and `roots`.
  - 协议节点: `fuzzyFileSearch -> fuzzyFileSearch/sessionUpdated* -> fuzzyFileSearch/sessionCompleted`
  - 来源: OpenAI 官方页面 `Misc methods`, `Notifications`，以及 `tmp/appserver-schema/codex_app_server_protocol.v2.schemas.json`
- 我们当前实现:
  - 没有任何通知处理。
- 差异点:
  - 该能力与当前场景无关，因此整条链路不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要独立 fuzzy file search 时再实现。

### SM-20 `AccountAuthLifecycle`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `account/login/start`、`account/login/completed`、`account/updated`、`account/login/cancel`、`account/chatgptAuthTokens/refresh`
  - schema 原始定义: `When true, requests a proactive token refresh before returning.`
  - schema 原始定义: `Clients should refresh tokens themselves and call account/login/start with chatgptAuthTokens.`
  - 协议节点: `account/login/start -> account/login/completed -> account/updated`，另有 `account/login/cancel` 与 `account/chatgptAuthTokens/refresh`
  - 来源: OpenAI 官方页面 `Authentication methods`，以及 `tmp/appserver-schema/codex_app_server_protocol.v2.schemas.json`、`tmp/appserver-schema/ServerRequest.json`
- 我们当前实现:
  - 没有对应 client 方法或通知处理。
- 差异点:
  - 该能力与当前场景无关，因此整条链路不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要由 Feidex 承担账户登录管理时，再完整实现。

### SM-21 `McpOAuthLoginLifecycle`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `mcpServer/oauth/login`、`mcpServer/oauthLogin/completed`
  - schema 原始定义: `McpServerOauthLoginParams` requires `name`; optional `scopes` and `timeoutSecs`.
  - 协议节点: `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`
  - 来源: OpenAI 官方页面 `Authentication methods`，以及 `tmp/appserver-schema/codex_app_server_protocol.v2.schemas.json`
- 我们当前实现:
  - 没有对应方法或通知处理。
- 差异点:
  - 该能力与当前场景无关，因此整条链路不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要在 Feidex 侧完成 MCP server 登录时，再补。

### SM-22 `PermissionsApproval`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面未展开；这里只能引用 schema 原始定义。
  - schema 原始定义: `Request approval for additional permissions from the user.`
  - method: `item/permissions/requestApproval`
  - params: `itemId`, `permissions`, `threadId`, `turnId`, optional `reason`
  - schema 原始定义: `permissions` 使用 `RequestPermissionProfile`，可包含 `fileSystem` 和 `network`。
  - 协议节点: `item/permissions/requestApproval -> client decision -> serverRequest/resolved`
  - 来源: `tmp/appserver-schema/ServerRequest.json`、`tmp/appserver-schema/codex_app_server_protocol.schemas.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 会接住请求并展示卡片。
  - `internal/app/serverrequest/approval.go` 会回 `permissions` 和 `scope`。
  - `internal/app/server_request_state.go` 已把 permissions approval 纳入 `pending -> replied -> resolved` 两阶段状态机，并等待 `serverRequest/resolved` 再恢复 submission。
  - `internal/app/approval/permission_summary.go` 会显式渲染 `RequestPermissionProfile.fileSystem` / `network` 结构，并兼容旧字段摘要。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-23 `McpElicitationRequest`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面未展开；这里只能引用 schema 原始定义。
  - schema 原始定义: `Request input for an MCP server elicitation.`
  - method: `mcpServer/elicitation/request`
  - mode 可以是 `form` 或 `url`
  - schema 原始定义: `McpElicitationSchema` 是 `Typed form schema for MCP elicitation/create requests.`
  - schema 原始定义: `turnId` 是可选关联信息，因为 MCP 把 elicitation 建模为独立的 server-to-client request。
  - 协议节点: `mcpServer/elicitation/request -> client response -> serverRequest/resolved`
  - 来源: `tmp/appserver-schema/ServerRequest.json`、`tmp/appserver-schema/codex_app_server_protocol.schemas.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 有 form/url 两种处理。
  - `internal/app/serverrequest/elicitation.go` 与 `internal/app/submission/pending_forms.go` 现在只在 reply 成功后把请求推进到 `replied`。
  - `internal/app/server_request_state.go` 统一等待 `serverRequest/resolved` 才最终 resolve 并恢复 submission。
  - 本次新增 MCP send tool 只复用独立的本地 MCP HTTP server，不改动 `mcpServer/elicitation/request` 的 pending / reply / resolved 契约。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-24 `SkillsCatalogLifecycle`

- 结论: `兼容实现`
- OpenAI 原始要求:
  - 官方页面/Schema 原始协议名与约束: `skills/list`
  - schema 原始定义: `forceReload` 为 true 时应绕过缓存并重新扫描 skills。
  - schema 原始定义: `perCwdExtraUserRoots` 可为每个 cwd 追加 user-scoped skill roots。
  - 官方页面/Schema 原始协议名与约束: `skills/changed`
  - schema 原始定义: `skills/changed` 是本地 skill 文件变化的失效通知；需要时应按当前参数重新执行 `skills/list`。
  - 协议节点: `skills/list -> response`；本地 skill 文件变化时 `skills/changed -> skills/list(refresh)`
  - 来源: `tmp/appserver-schema/ClientRequest.json`、`tmp/appserver-schema/ServerNotification.json`、`tmp/appserver-schema/v2/SkillsListParams.json`
- 我们当前实现:
  - `internal/codexrpc/skills.go` 定义 `SkillsListResult`、`SkillsListEntry`、`SkillMetadata` 等 response 类型。
  - `internal/app/skillscmd/service.go` 的 `/skills` 会按当前 workspace cwd 调用 `skills/list`；`/skills reload` 和 `skills.reload` 会带 `forceReload=true`。
  - `skills.select` 会重新读取当前 cwd 的 skills，拒绝 disabled skill，并把选中的 skill 存为当前 session 的 pending skill。
  - `ResolveSubmissionSkill` 支持 `$skill-name <内容>` 显式前缀和 pending skill；最终 `turn/start` input 会把 `type=skill` 放在文本输入前，并在创建 submission 后消费 pending skill。
  - `internal/app/features/data.go` 把 `/skills`、`/skills reload`、`$skill-name <内容>` 和 `menu.skills` 作为 Codex-only 本地能力暴露；Claude backend 隐藏并 passthrough。
  - `internal/app/codex_event_router.go` 仍没有 `skills/changed` 通知分支。
  - `internal/app/apphistory/history.go` 会在 `/history` 里把 `thread/read` 返回的 `userMessage.content[type=skill]` 显示为 `[skill] ...`。
- 差异点:
  - 当前没有客户端侧 skills catalog 长期缓存，也不会在收到 `skills/changed` 时主动刷新已打开的 skills 卡片；当前 UX 依赖用户重新打开 `/skills` 或显式 `/skills reload`。
  - `perCwdExtraUserRoots` 当前没有配置或 UI 入口。
  - 这些差异不影响当前“浏览当前 workspace skills、选择下一条消息 skill、显式 skill prefix”的主路径。
- 修改建议:
  - 若后续需要 skill 文件变更后自动刷新已打开卡片，应消费 `skills/changed`，记录最近一次 list 参数，并 patch 受影响的 `menu.skills` 卡片。
  - 若后续需要跨 cwd 追加 user-scoped skill roots，应补配置、UI 和 `perCwdExtraUserRoots` payload 测试。

### SM-25 `ThreadGoalLifecycle`

- 结论: `兼容实现`
- OpenAI / 本地 Codex 原始要求:
  - 协议名与约束: `thread/goal/set` 为 materialized thread 创建或更新单个持久 goal，返回当前 goal，并在状态变化时发出 `thread/goal/updated`。
  - 协议名与约束: `thread/goal/get` 读取当前 goal；没有 goal 时返回 `goal: null`。
  - 协议名与约束: `thread/goal/clear` 清除当前 goal，返回是否实际移除，并在状态变化时发出 `thread/goal/cleared`。
  - schema 原始定义: `ThreadGoalSetParams.tokenBudget` 是 optional nullable number；省略表示不改，`null` 表示清空预算，number 表示设置预算。
  - 通知原始定义: `thread/goal/updated` 包含完整 `goal`，`thread/goal/cleared` 只包含 `threadId`。
  - 协议节点: `thread/goal/set|get|clear -> response`；goal 状态变化时 `thread/goal/updated|cleared`；active goal 可能由后端主动继续并发出没有本地 `turn/start` 对应的 `turn/started`。
  - 来源: `/home/yuhuan/codex/codex-rs/app-server/README.md`、`/home/yuhuan/codex/codex-rs/app-server-protocol/src/protocol/v2/thread.rs`
- 我们当前实现:
  - `internal/codexrpc/goal.go` 定义 `ThreadGoal`、goal 请求/响应/通知类型，并用 `NullableInt64` 表达 optional nullable `tokenBudget`。
  - `internal/app/goal.go` 通过 `/goal` 暴露 get/set/pause/resume/clear/edit；`/goal <objective>` 不解析 `--tokens`，整段尾部文本按 objective 发送；无参数 `/goal` 在没有当前 goal 时渲染 objective 输入表单，提交后创建 active goal。
  - `internal/app/features/data.go` 把 `任务目标` 加入常用工具菜单，并保留直接 `/goal` 命令入口；Claude backend 隐藏并 passthrough。
  - `internal/app/codex_event_router.go` / `internal/app/backend/codex_event_router.go` 消费 `thread/goal/updated` 和 `thread/goal/cleared`，更新 frontend 内存 tracker。
  - `internal/app/turnlifecycle/service.go` 在普通 pending submission 和 standalone compact 都未绑定时，尝试用 active goal tracker 将 orphan `turn/started` 合成为本地 `kind=goal` submission，并绑定 `threadId + turnId`，后续 item/turn 仍沿用标准 turn 生命周期。
  - goal continuation 绑定前会先主动发送一张新的 Feishu outbound card，header 简洁显示 goal continuation 的 `Turn #N` 和 objective，正文只显示耗时与 token 统计；返回的 message ID 作为合成 submission 的 `TriggerMessageID`、唯一 source root，以及后续 turn item 的回复锚点。
  - `/goal` status/set/edit/replace/pause/resume/clear 产生的管理卡只记录 session/chat 上下文，不记录 message ID；Codex turn 输出永远不能回复到 goal 管理卡。
- 差异点:
  - v1 Feishu UI 不提供设置或清空 token budget 的命令参数；edit 只会保留已有 budget。协议类型已支持 explicit null，后续如果 UI 暴露预算清除，不需要改 wire type。
  - active goal continuation 的本地 submission 是 Feidex 合成的展示/状态锚点，不对应本地 `turn/start` 请求；它只在 tracker 里存在 active goal 且同 thread session 空闲时创建，避免抢占正在进行的本地工作。由于每个后台驱动 continuation 没有 Feishu inbound，Feidex 必须为每个 continuation turn 创建新的 outbound 根消息，不能复用此前用户 inbound、session root 或任何 goal 管理卡；这同样适用于设置新 goal 后的第一个 turn。
  - goal tracker 是进程内缓存；重启后只有用户再次 `/goal` 或收到 goal 通知才会恢复 active goal 观察。
- 修改建议:
  - 保持 v1 实现即可；若后续需要跨重启自动接管 active goal continuation，应把 goal tracker 持久化或在启动/resume 时对活跃 thread 执行 `thread/goal/get`。
