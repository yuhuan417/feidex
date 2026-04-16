# Codex App Server 状态机审计

审计时间: 2026-04-16

官方来源:
- `https://developers.openai.com/codex/app-server`

本地对照来源:
- `internal/codexrpc/client.go`
- `internal/app/codex_event_router.go`
- `internal/app/app.go`
- `internal/app/submission_queue.go`
- `internal/app/turn_lifecycle.go`
- `internal/app/approval_actions.go`
- `internal/app/approval_summaries.go`
- `internal/app/tool_user_input_forms.go`
- `internal/app/user_input_actions.go`
- `internal/app/pending_forms.go`
- `internal/app/elicitation_forms.go`
- `internal/app/compact.go`
- `tmp/appserver-schema/`

## 审计口径

- `严格遵循`: 请求/通知顺序、状态边界、最终确认边界都和官方文档一致，客户端不会提前推进本地状态。
- `兼容实现`: 核心交互节点按正确时序处理，最终结果完整；只是刻意忽略某些中间态、流式增量或冗余通知，不影响用户信息获取和交互成功。
- `部分遵循`: 主路径能跑通，但遗漏了官方要求的某些通知、状态边界或决策分支，存在“看起来能用，但不是严格按协议”的情况。
- `未遵循`: 当前实现会错过、误处理、提前结束或直接拒绝官方描述的流程。
- `未实现`: 官方文档里有该流程，但 Feidex 既没有发起该方法，也没有消费对应通知/请求。
- 本文只审计 app-server 的方法、通知、server request 及其生命周期，不单独审计 `thread/read` 这类结果 payload 的枚举字段。
- 因此像 `userMessage.content` 里的 `skill`、`mention`、`localImage` 这类输入项类型，属于历史/展示层兼容范围，不单列为 `SM-xx` 状态机。

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

## 逐项审计

### SM-01 `InitializeHandshake`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原文: `Initialize once per connection`
  - 官方页面原文: `Clients must send a single initialize request per transport connection.`
  - 协议节点: `initialize -> initialized`
  - 来源: OpenAI 官方页面 `Initialization`
- 我们当前实现:
  - `internal/codexrpc/client.go:89-123` 在 transport 启动后立即发送 `initialize`，收到响应后立刻发送 `initialized`。
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
  - `internal/codexrpc/client.go:113-119` 会在 `initialize` 的 `capabilities` 中声明 `experimentalApi` 与 `optOutNotificationMethods`。
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
  - `internal/app/submission_queue.go:193-225` 发 `thread/start`。
  - `internal/app/thread_feature_actions.go:132-156` 发 `thread/resume`。
  - `internal/app/fork.go:57-90` 发 `thread/fork`。
  - 上述三条主链路都直接使用 RPC response 中返回的 thread 信息更新本地 session。
  - `internal/app/codex_event_router.go:28-133` 没有 `thread/started`、`thread/archived`、`thread/unarchived`、`thread/closed`、`thread/status/changed` 的处理分支。
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
  - `internal/app/app.go:177-221` 发送 `turn/start`。
  - `internal/app/submission_queue.go:248-305` 和 `internal/app/turn_lifecycle.go:10-122` 会处理返回的 `turn.id` 以及 `turn/started`。
  - `internal/app/codex_event_router.go` 现在同时消费 `item/started` 与 `item/completed`。
  - `internal/app/turn_item_state.go` 为每个 `turnId + itemId` 维护 started/completed 快照，并在 completed 时合并成最终 item 载荷。
  - `internal/app/codex_event_router.go:57-87` 消费 `turn/started` 和 `turn/completed`。
  - `internal/app/turn_item_payload.go:21-67` 按 completed item 渲染最终内容。
  - `internal/codexrpc/client.go:113-125` 通过 `optOutNotificationMethods` 明确退订当前不接入的 item delta 通知。
- 差异点:
  - `item/agentMessage/delta`、`item/plan/delta`、`item/commandExecution/outputDelta`、`item/fileChange/outputDelta` 等流式通知被明确视为非目标能力，并通过 opt-out 退订。
  - `item/started` 已经接入，因此 tool approval / file change 这类需要前置 item 上下文的流程，已经不再只依赖 completed item。
  - 当前产品以 started/completed 为唯一消费边界，最终 completed item 仍被完整处理，没有用户可见信息损失。
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
  - `internal/app/steer.go:127-137` 和 `internal/app/thread_menu.go:306-320` 都会带 `expectedTurnId`。
  - steer 后续沿用 started/completed 两阶段处理，流式 delta 不接入，并依赖 `internal/codexrpc/client.go:113-125` 中的 opt-out 退订相关通知。
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
  - `internal/app/thread_menu.go:281-303` 发送 `turn/interrupt`。
  - `internal/app/turn_lifecycle.go:150-156` 在 `turn/completed` 时把 `interrupted` 作为最终状态写回。
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
  - `internal/app/codex_event_router.go:101-122` 记录 `error`。
  - `internal/app/turn_lifecycle.go:150-156` 最终仍在 `turn/completed` 处 finalize。
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
  - `internal/app/compact.go:43-69` 发送 `thread/compact/start`。
  - `internal/app/turn_lifecycle.go:45-46` / `internal/app/compact.go:72-100` 用 `turn/started` 和 `item/started(type=contextCompaction)` 绑定 standalone compact turn。
  - `internal/app/turn_stream.go:49-57` / `internal/app/compact.go:102-180` 用 `item/completed(type=contextCompaction)` 收口 standalone compact turn，后续 `turn/completed` 只做兜底。
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
  - `internal/app/approval_actions.go` 已支持 `accept`、`acceptForSession`、`decline`、`cancel` 四类 decision，并且只在 reply 成功后把本地请求推进到 `replied`。
  - `internal/app/server_request_state.go` 把 `serverRequest/resolved` 作为唯一 `resolved` 边界，并在同 turn 其他 open request 清空后才恢复 submission。
  - `internal/app/codex_event_router.go` 仍会在最终 `item/completed` 时收口 item。
- 差异点:
  - command approval 的生命周期边界已经对齐。
  - schema 里虽然还有 `acceptWithExecpolicyAmendment`、`applyNetworkPolicyAmendment` 两类 amendment decision，但当前产品已明确不做这类 persistent amendment，因此不视为实现缺口。
- 修改建议:
  - 保持现状即可；仅当后续产品重新需要 persistent exec/network policy amendment 时，再把对应 decision 与 UI 一起补回。

### SM-10 `FileApproval`

- 结论: `严格遵循`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `item/started` emits a `fileChange` item.
  - 官方页面原始协议名与约束: `item/fileChange/requestApproval` asks the client for approval.
  - 官方页面原始协议名与约束: client 回复后，server 会发 `serverRequest/resolved`。
  - 官方页面原始协议名与约束: 最终由 `item/completed` 收口。
  - 协议节点: `item/started(fileChange) -> item/fileChange/requestApproval -> client decision -> serverRequest/resolved -> item/completed`
  - 来源: OpenAI 官方页面 `Tool approvals and requests`，以及 `tmp/appserver-schema/ServerRequest.json`
- 我们当前实现:
  - `internal/app/codex_event_router.go` 会先接 `item/started(fileChange)`，再处理 `item/fileChange/requestApproval`。
  - `internal/app/turn_item_state.go` 会把 started item 上的 `changes` 合并进审批请求，文件审批卡片可从 started item 补齐缺失文件列表。
  - `internal/app/approval_actions.go` 和 `internal/app/server_request_state.go` 已支持 `accept`、`acceptForSession`、`decline`、`cancel` 四类 decision，并改成 `pending -> replied -> resolved`，等待 `serverRequest/resolved` 再最终收口。
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
  - `internal/app/codex_event_router.go:201-213` 收到 request 后发送 UI。
  - `internal/app/user_input_actions.go`、`internal/app/tool_user_input_forms.go`、`internal/app/pending_forms.go` 现在只在 reply 成功后把请求推进到 `replied`。
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
  - `internal/app/codex_event_router.go:136-152` 没有 `item/tool/call` 分支，默认直接 `ReplyError(... unsupported server request)`。
- 差异点:
  - 当前将该状态机视为 experimental 范围外能力，不纳入实现范围。
  - 因此 `item/tool/call` 仍会被拒绝，`item/started(dynamicToolCall)` 也没有单独接入层。
- 修改建议:
  - 暂不实现；仅当后续产品明确决定支持 experimental dynamic tools 时，再补齐整条生命周期。

### SM-14 `ReviewLifecycle`

- 结论: `未实现`
- OpenAI 原始要求:
  - 官方页面原始协议名与约束: `review/start` starts a review.
  - 官方页面原始协议名与约束: detached review 会先新建 thread，并通过 `thread/started` 报告。
  - 官方页面原始协议名与约束: review mode 通过 `enteredReviewMode` / `exitedReviewMode` item 表示。
  - 协议节点: `review/start -> thread/started? -> turn/started -> item/started(enteredReviewMode) -> ... -> item/completed(exitedReviewMode) -> turn/completed`
  - 来源: OpenAI 官方页面 `Turn methods`, `Notifications`
- 我们当前实现:
  - 没有 `review/start` 调用。
  - 没有 review item 生命周期处理。
- 差异点:
  - 整条状态机未接入。
- 修改建议:
  - 如果产品要支持 review，再单独实现；否则保持未实现即可。

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
  - `internal/app/codex_event_router.go:144-199` 会接住请求并展示卡片。
  - `internal/app/approval_actions.go:48-60` 会回 `permissions` 和 `scope`。
  - `internal/app/server_request_state.go` 已把 permissions approval 纳入 `pending -> replied -> resolved` 两阶段状态机，并等待 `serverRequest/resolved` 再恢复 submission。
  - `internal/app/permission_summary.go` 会显式渲染 `RequestPermissionProfile.fileSystem` / `network` 结构，并兼容旧字段摘要。
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
  - `internal/app/codex_event_router.go:148-149` / `215-240` 有 form/url 两种处理。
  - `internal/app/elicitation_forms.go` 与 `internal/app/pending_forms.go` 现在只在 reply 成功后把请求推进到 `replied`。
  - `internal/app/server_request_state.go` 统一等待 `serverRequest/resolved` 才最终 resolve 并恢复 submission。
- 差异点:
  - 无。
- 修改建议:
  - 无需修改。

### SM-24 `SkillsCatalogLifecycle`

- 结论: `暂不实现`
- OpenAI 原始要求:
  - 官方页面/Schema 原始协议名与约束: `skills/list`
  - schema 原始定义: `forceReload` 为 true 时应绕过缓存并重新扫描 skills。
  - schema 原始定义: `perCwdExtraUserRoots` 可为每个 cwd 追加 user-scoped skill roots。
  - 官方页面/Schema 原始协议名与约束: `skills/changed`
  - schema 原始定义: `skills/changed` 是本地 skill 文件变化的失效通知；需要时应按当前参数重新执行 `skills/list`。
  - 协议节点: `skills/list -> response`；本地 skill 文件变化时 `skills/changed -> skills/list(refresh)`
  - 来源: `tmp/appserver-schema/ClientRequest.json`、`tmp/appserver-schema/ServerNotification.json`、`tmp/appserver-schema/v2/SkillsListParams.json`
- 我们当前实现:
  - 没有 `skills/list` 对应 client 调用。
  - `internal/app/codex_event_router.go` 没有 `skills/changed` 通知分支。
  - `internal/app/history.go` 仅在 `/history` 里把 `thread/read` 返回的 `userMessage.content[type=skill]` 显示为 `[skill] ...`，不等同于 skill catalog 管理。
- 差异点:
  - 当前不会主动读取 skill catalog，也不会在 skill 文件变化后刷新 catalog。
  - 该能力与当前场景无关，因此不纳入实现范围。
- 修改建议:
  - 暂不实现；仅当后续产品明确需要浏览、刷新或管理 skills catalog 时，再补齐 `skills/list` / `skills/changed`。
