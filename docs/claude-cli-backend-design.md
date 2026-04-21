# Claude CLI Backend Design

状态: Draft  
日期: 2026-04-20

## 背景

目标是让同一个 Feishu 前端同时支持两种后端:

- 当前已支持的 Codex App Server
- 新增的 Claude CLI `stream-json` 后端

这里的关键结论要先说清楚:

- Claude CLI `stream-json` 不能被视为 Codex app-server 的协议兼容实现。
- Claude CLI 可以在 Feidex 产品层能力上做到“部分对齐”，前提是引入一层明确的 backend adapter，把协议差异吸收在后端侧，而不是继续让 `internal/app` 直接消费 Codex 风格的方法名和 server request 语义。
- Codex 路径仍然必须继续遵守 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)；这个约束不能因为引入第二后端而被稀释。

## 参考输入

- [DEVELOPER.md](/home/yuhuan/feidex/DEVELOPER.md)
- [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)
- [internal/app/deps.go](/home/yuhuan/feidex/internal/app/deps.go)
- [internal/app/codex_event_router.go](/home/yuhuan/feidex/internal/app/codex_event_router.go)
- [internal/app/turn_item_state.go](/home/yuhuan/feidex/internal/app/turn_item_state.go)
- [internal/config/config.go](/home/yuhuan/feidex/internal/config/config.go)
- [internal/state/store.go](/home/yuhuan/feidex/internal/state/store.go)
- [internal/app/workspace_threads.go](/home/yuhuan/feidex/internal/app/workspace_threads.go)
- [internal/app/history.go](/home/yuhuan/feidex/internal/app/history.go)
- [internal/app/review.go](/home/yuhuan/feidex/internal/app/review.go)
- [internal/app/compact.go](/home/yuhuan/feidex/internal/app/compact.go)
- [internal/app/skills.go](/home/yuhuan/feidex/internal/app/skills.go)
- [claude-cli-protocol/protocol/docs/PROTOCOL_SPECIFICATION.md](/home/yuhuan/feidex/claude-cli-protocol/protocol/docs/PROTOCOL_SPECIFICATION.md)
- [claude-cli-protocol/sdks/golang/protocol/messages.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/protocol/messages.go)
- [claude-cli-protocol/sdks/golang/protocol/stream.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/protocol/stream.go)
- [claude-cli-protocol/sdks/golang/protocol/control.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/protocol/control.go)
- [claude-cli-protocol/sdks/golang/claude/session.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/claude/session.go)
- [claude-cli-protocol/sdks/golang/claude/interactive.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/claude/interactive.go)
- [claude-cli-protocol/sdks/golang/claude/session_options.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/claude/session_options.go)
- [claude-cli-protocol/sdks/golang/claude/process.go](/home/yuhuan/feidex/claude-cli-protocol/sdks/golang/claude/process.go)

## 非目标

这份设计不做以下承诺:

- 不把 Claude backend 伪装成完整的 Codex JSON-RPC server。
- 不承诺在第一阶段实现 `review/start`、`thread/compact/start`、`thread/list`、`thread/fork`、`thread/read`、`skills/list` 的全量等价能力。
- 不在当前阶段修改 Feishu 前端交互形态。
- 不在当前阶段直接开始产品代码实现。

## 设计护栏

这次双后端设计必须同时满足下面两个原则:

### 1. 不是“只做交集”

backend-neutral 抽象只覆盖共享的产品骨架，不代表最终产品能力只能保留两个后端的交集。

共享骨架应覆盖:

- 会话绑定
- turn 生命周期
- 流式输出
- 工具调用
- 审批 / 用户输入
- usage / cost

但 backend-specific 能力仍然必须是一等能力，可以继续增长，不应该因为另一后端暂时没有等价物而被压平或取消。

例子:

- Codex 的 `review/start`、`thread/compact/start`、`skills/list` 即使 Claude 没有，也仍然可以作为 Codex 一等能力保留。
- Claude 后续如果暴露出 Codex 没有的计划模式、session tooling 或其他专有控制能力，也应当能以 Claude 一等能力接入，而不是先要求 Codex 也有对应协议。

### 2. 不是“一个 backend 依附另一个 backend”

这层抽象不能以某一方的协议为中心，然后让另一方去“假装自己是它”。

具体要求:

- 不能把 Claude 设计成“模拟 Codex RPC”的从属实现。
- 也不能把 Codex 设计成“仅仅为了适配 Claude 公共抽象”而丢掉自己成熟的原生能力。
- backend-neutral 层只负责共享骨架和公共编排。
- backend-specific 层负责各自独有能力，并且这些能力可以被 Feishu 前端直接暴露。

## 现状判断

### 1. 传输接口很薄，但产品语义高度 Codex 化

[internal/app/deps.go](/home/yuhuan/feidex/internal/app/deps.go) 中 `codexClient` 接口只有几个通用方法:

- `SetHandlers`
- `Start`
- `Close`
- `Call`
- `Reply`
- `ReplyError`

但 `internal/app` 实际依赖的是一整套 Codex 专属协议语义:

- 请求方法: `thread/start`、`thread/resume`、`turn/start`、`turn/steer`、`thread/read`、`thread/list`、`review/start`、`thread/compact/start`、`skills/list`、`model/list`
- 通知方法: `item/started`、`item/completed`、`turn/plan/updated`、`turn/started`、`turn/completed`、`thread/tokenUsage/updated`、`serverRequest/resolved`
- server request: `item/commandExecution/requestApproval`、`item/fileChange/requestApproval`、`item/permissions/requestApproval`、`item/tool/requestUserInput`、`mcpServer/elicitation/request`

[internal/app/codex_event_router.go](/home/yuhuan/feidex/internal/app/codex_event_router.go) 和 [internal/app/turn_item_state.go](/home/yuhuan/feidex/internal/app/turn_item_state.go) 已经把这些语义写进了产品层。

### 2. Claude CLI stream-json 是另一种协议模型

根据 [claude-cli-protocol/protocol/docs/PROTOCOL_SPECIFICATION.md](/home/yuhuan/feidex/claude-cli-protocol/protocol/docs/PROTOCOL_SPECIFICATION.md) 和 Go SDK 类型:

- Claude 走的是 stdio NDJSON，不是 JSON-RPC。
- 顶层消息类型是 `system`、`assistant`、`user`、`result`、`stream_event`、`control_request`、`control_response`。
- 会话建立依赖 `system subtype=init`。
- turn 不是显式 RPC 对象，而是“发送一条 user message 后产生的一次处理”。
- 流式输出来自 `stream_event`:
  - `message_start`
  - `content_block_start`
  - `content_block_delta`
  - `content_block_stop`
  - `message_delta`
  - `message_stop`
- 权限控制来自 `control_request subtype=can_use_tool`。
- 中断可以通过 control request / SDK `Interrupt` 实现。
- 恢复依赖 CLI 参数 `--resume <session_id>`。
- 交互式工具不是 Codex 风格 `requestUserInput`，而是 `AskUserQuestion`、`ExitPlanMode` 这类 tool use。

结论: Claude 可以承载“会话 + turn + tool + approval + resume”这类产品语义，但不是 Codex 那种原生 thread/turn/serverRequest 生命周期。

## 能力对齐结论

### 总结

- 能做到产品层可用对齐: 基础聊天、多轮会话、流式输出、工具调用可视化、中断、权限审批、用户问题、会话恢复、usage/cost 展示。
- 需要语义适配而不是一比一映射: turn/item 生命周期、server request 完成边界、plan 更新、工具输入流式展示。
- 第一阶段不建议承诺对齐: review、context compaction、thread catalog、thread fork、skills catalog、Codex 风格历史读取。

### 能力矩阵

| Feidex 能力 | 当前 Codex 依赖 | Claude 协议原语 | 结论 | 说明 |
| --- | --- | --- | --- | --- |
| 新建会话 | `thread/start` | 启动 CLI + `system:init` + 首条 `user` 消息 | `适配可行` | Claude 没有独立 `thread/start`，需要 adapter 以 `session_id` 作为 conversation handle。 |
| 恢复会话 | `thread/resume` | `--resume <session_id>` | `适配可行` | 可恢复上下文，但不是 Codex thread resume 语义。 |
| 发起 turn | `turn/start` | 发送 `user` message | `适配可行` | turn id 需要 Feidex 或 adapter 本地生成。 |
| turn 生命周期 | `turn/started` / `turn/completed` | `stream_event` + `result` | `适配可行` | `turn/started` 需合成，`result` 作为主要 completed 边界。 |
| 文本流式输出 | `item/*` 或最终 `item/completed` | `content_block_delta(text_delta)` | `适配可行` | Claude 在这方面反而更直接。 |
| thinking / reasoning | reasoning item 或增量 | `content_block_delta(thinking_delta)` | `适配可行` | 是否展示仍由产品 quiet mode 决定。 |
| 工具调用进度 | `item/started` / `item/completed` | `tool_use` content block + `input_json_delta` | `适配可行` | 可合成为 backend-neutral tool item 生命周期。 |
| 权限审批 | `requestApproval` + `serverRequest/resolved` | `control_request(can_use_tool)` + `control_response` | `适配可行` | 没有原生 `resolved` 通知，需要 adapter 合成“可继续”边界。 |
| 用户输入问题 | `item/tool/requestUserInput` | `AskUserQuestion` | `部分对齐` | 支持问答，但字段形状不同。 |
| plan 审批 / 退出计划模式 | `turn/plan/updated` + user input | `ExitPlanMode` | `部分对齐` | 更接近“一次性计划确认”，不是连续 plan delta。 |
| usage / 成本 | `thread/tokenUsage/updated` | `message_delta.usage` + `result.usage` + `total_cost_usd` | `适配可行` | 可做 turn 内更新和 turn 结束汇总。 |
| 中断 | `turn/interrupt` | interrupt control | `适配可行` | 需要在 neutral layer 抽象为 `InterruptTurn`。 |
| 模型列表 | `model/list` | 无稳定 catalog，只能看到当前 model / set-model | `不对齐` | Claude 第一阶段不支持当前 `model/list` UI。 |
| reasoning effort | `model/list` metadata | 无明确等价项 | `不对齐` | Claude 没有 Codex 风格 reasoning effort 枚举。 |
| skills 列表 | `skills/list` | `system:init.skills[]` 仅名称级信息 | `不对齐` | 缺少 cwd/scope/enabled/reload 等产品需要的信息。 |
| 历史读取 | `thread/read` | 无等价原语 | `不直接对齐` | 需 Feidex 自建 transcript 才能后续补齐。 |
| thread 列表 | `thread/list` | 无等价原语 | `不对齐` | 只能依赖 Feidex 自己的会话索引。 |
| thread fork | `thread/fork` | 无等价原语 | `不对齐` | 第一阶段不做。 |
| context compaction | `thread/compact/start` | 只观察到 slash command `compact` | `不对齐` | 没有稳定程序化协议，不应假设可用。 |
| review | `review/start` | 只观察到 slash command `review` / `security-review` | `不对齐` | 在没有稳定 wire primitive 前不接入。 |
| MCP elicitation | `mcpServer/elicitation/request` | 未看到等价原语 | `不对齐` | 需要单独评估。 |

## 推荐架构

### 设计原则

1. `internal/app` 只能依赖 backend-neutral 的产品事件和命令。
2. Codex 专属协议细节只能停留在 Codex adapter 中。
3. Claude 专属协议细节只能停留在 Claude adapter 中。
4. Codex 路径继续以状态机审计为准，不因抽象层引入而改变行为。
5. 不支持的能力必须显式 capability-gate，而不是“先接个半成品再说”。

### 包结构建议

建议引入一个新的 backend 边界:

```text
internal/
  backend/
    types.go          # backend-neutral event/request/capability model
    factory.go        # 按 frontend/runtime 选择 backend
    extensions.go     # backend-specific feature descriptors / handlers
    codex/
      adapter.go      # 包装 internal/codexrpc
      mapper.go       # Codex notification/server-request -> neutral events
      capabilities.go
    claude/
      adapter.go      # Claude backend facade
      mapper.go       # stream/control messages -> neutral events
      permissions.go  # can_use_tool / AskUserQuestion / ExitPlanMode
      session.go      # backend session lifecycle
      transcript.go   # Claude 本地 transcript/summary 组装
  codexrpc/           # 保持 transport/protocol-focused
  claudecli/          # 如需自研 transport，可放原始 NDJSON/stdio client
```

说明:

- [internal/codexrpc](/home/yuhuan/feidex/internal/codexrpc) 不应该继续直接暴露到 `internal/app`。
- Claude backend 最好也有一层“原始协议客户端”和“一层产品 adapter”分离，这样后续协议漂移时影响面更小。
- [internal/app/codex_event_router.go](/home/yuhuan/feidex/internal/app/codex_event_router.go) 最终应演进成 backend-neutral router，而不是继续硬编码 Codex 方法名。
- `internal/backend/extensions.go` 这类层不是可选装饰，而是防止“最后只剩交集能力”的关键结构。

### backend-neutral 契约

建议抽象的不是“模仿 Codex JSON-RPC”，而是 Feidex 真正需要的产品语义:

- 会话已就绪
- 会话已绑定到当前 Feishu session
- turn started
- 输出文本 delta
- thinking delta
- tool call started / progressed / completed
- 审批请求到达
- 用户输入请求到达
- plan 可确认
- usage 更新
- turn completed
- backend error

对应的 control-plane 命令建议是:

- `StartConversation`
- `ResumeConversation`
- `SendTurn`
- `ContinueTurn`
- `InterruptTurn`
- `ReplyApproval`
- `ReplyUserInput`
- `SetModel`
- `CloseConversation`

不要把 neutral layer 设计成 `Call(method string, params any)` 这种弱约束接口，否则 `internal/app` 会继续泄漏回 Codex 方法名。

### capability model

同一个 Feishu 前端要同时支持两种后端，必须把能力拆成两层，而不是只做一个 bool capability 表。

第一层是共享核心能力，用于驱动统一的聊天主流程。

第二层是扩展能力，用于驱动 backend-specific 的菜单、命令和卡片动作。

建议结构:

```text
CoreCapabilities
  ConversationResume
  Interrupt
  StreamingOutput
  ToolApproval
  UserInput
  Usage

ExtensionCapabilities
  codex.review
  codex.compact
  codex.skills_catalog
  codex.model_catalog
  claude.plan_mode
  claude.session_resume_catalog
  ...
```

共享层的能力声明仍然需要，例如:

```text
ConversationResume
ConversationList
HistoryRead
Fork
Compaction
InlineReview
SkillCatalog
ModelCatalog
ReasoningEffort
PlanUpdates
McpElicitation
```

产品层规则:

- 对共享能力，capability 为 false 时在统一主流程里显式降级。
- 对扩展能力，不要求所有 backend 都实现；支持该扩展的 backend 可以正常暴露该功能。
- Feishu 前端需要同时支持“公共入口”和“backend-specific 入口”，而不是只保留公共入口。
- 降级行为必须稳定可解释，例如“当前 backend 不支持 review”。
- 不要让用户点进去之后走到协议错误。

换句话说:

- capability gate 是为了安全降级，不是为了抹平差异。
- extension registry 是为了让每个 backend 能继续长自己的能力树。

### 前端暴露模型

同一个 Feishu 前端不等于“所有卡片完全相同”。

推荐做法是把前端暴露拆成三层:

1. 共享主入口
   例如发送消息、查看进行中状态、中断、审批、基本历史。
2. backend-aware 入口
   根据当前 frontend 绑定的 backend 展示“Codex 工具”或“Claude 工具”分组。
3. 扩展命令入口
   允许 backend 注册自己的命令和菜单动作，而不是把所有行为都塞进共享命令集合。

这样可以同时满足:

- 同一个 Feishu 产品入口统一
- 具体能力不被压成最低公分母
- 用户能理解“当前这个会话挂的是哪种 backend，以及它有哪些专属能力”

## Claude backend 如何映射到 neutral events

### 会话绑定

- Claude CLI 启动成功后，收到 `system subtype=init`。
- `session_id` 作为 Claude conversation handle 持久化。
- Feidex session 需要记录 backend kind=`claude` 和 backend conversation id=`session_id`。

### turn 映射

- `SendTurn` 实际上是向 Claude stdin 写入一条 `user` message。
- adapter 在本地生成 `operation_id` 或 `turn_ref`，用于 Feidex 内部状态关联。
- 第一个有效 `stream_event` 或首个 `assistant` / `control_request` 到来时，发出 neutral `TurnStarted`。
- `result` 到来时发出 neutral `TurnCompleted`。

### 输出映射

- `content_block_delta(text_delta)` -> `OutputTextDelta`
- `content_block_delta(thinking_delta)` -> `ThinkingDelta`
- `message_delta.usage` -> `UsageUpdated`
- `result.usage` / `result.total_cost_usd` -> turn final usage summary

### tool 映射

- `content_block_start` 且 `content_block.type=tool_use` -> `ToolCallStarted`
- `content_block_delta(input_json_delta)` -> `ToolCallProgress`
- `content_block_stop` 或完整 assistant tool-use block -> `ToolCallCompleted`

这里不要求 Claude 强行伪装成 Codex `item/started` / `item/completed`，但 neutral layer 可以保留“started/completed”这一产品抽象。

### 审批映射

- `control_request(can_use_tool)` -> `ApprovalRequested`
- Feidex 审批卡回复后 -> 写回 `control_response`

这里要特别区分 Codex 和 Claude 的完成边界:

- Codex 有明确的 `serverRequest/resolved`，所以“请求已被 backend 处理并允许 turn 继续”有协议级确认。
- Claude 没有等价通知。

因此建议 neutral layer 使用更产品化的事件名，例如 `InteractionContinuationReady`:

- Codex adapter: 在收到 `serverRequest/resolved` 后发出。
- Claude adapter: 在 `control_response` 成功写入后发出 synthetic event，并明确这是“本地写入确认”，不是 Claude 原生 resolved 通知。

这样 `internal/app` 可以继续维持“审批处理完再恢复 submission”的保守语义，但不会直接绑定 `serverRequest/resolved` 这个 Codex 名称。

### 交互工具映射

- `AskUserQuestion` -> `UserInputRequested`
- `ExitPlanMode` -> `PlanApprovalRequested`

限制:

- `ExitPlanMode` 提供的是计划确认语义，不是 Codex 风格的连续 `turn/plan/updated` 增量流。
- 如果 Feidex 继续保留“执行中 plan 实时更新”卡片，Claude backend 第一阶段需要降级成“仅在退出计划模式时展示一次计划确认”。

## 对现有代码结构的影响

### `internal/app`

需要从“直接理解 Codex 协议”改成“消费 backend-neutral 事件”。

影响最大的点:

- [internal/app/deps.go](/home/yuhuan/feidex/internal/app/deps.go)
- [internal/app/codex_event_router.go](/home/yuhuan/feidex/internal/app/codex_event_router.go)
- [internal/app/workspace_threads.go](/home/yuhuan/feidex/internal/app/workspace_threads.go)
- [internal/app/history.go](/home/yuhuan/feidex/internal/app/history.go)
- [internal/app/review.go](/home/yuhuan/feidex/internal/app/review.go)
- [internal/app/compact.go](/home/yuhuan/feidex/internal/app/compact.go)
- [internal/app/skills.go](/home/yuhuan/feidex/internal/app/skills.go)
- [internal/app/model_config.go](/home/yuhuan/feidex/internal/app/model_config.go)

建议做法:

- 先把“事件消费层”抽出来。
- 再把 “thread/read / review / compact / skills / model list” 这类能力拆成共享能力和扩展能力两类。
- 不要一开始就试图把全部 feature 同时抽象掉。

更具体地说:

- `internal/app` 里统一聊天主流程应只依赖共享骨架。
- `review`、`compact`、`skills`、`model config` 这类明显 backend-specific 的工具流，应该迁到 backend extension registry 驱动。
- 后续如果 Claude 长出自己独有的高级能力，也应走同一套 extension registry，而不是再回到 `internal/app` 打协议分支。

### `internal/state`

[internal/state/store.go](/home/yuhuan/feidex/internal/state/store.go) 当前字段基本是 Codex 命名:

- `Session.ActiveThreadID`
- `Session.ActiveTurnID`
- `Submission.ThreadID`
- `Submission.TurnID`
- `PendingRequest.ThreadID`
- `PendingRequest.TurnID`
- `PendingRequest.ItemID`

推荐演进方向:

1. 新增 backend-neutral 字段，而不是第一步就强改旧字段名。
2. Codex backend 同时填充旧字段和新字段，保证迁移期兼容。
3. Claude backend 只填新字段。
4. 为 backend-specific 能力保留扩展状态槽，不要求所有后端共享同一份字段集。

建议新增:

- `BackendKind`
- `ConversationID`
- `OperationID`
- `InteractionID`
- `NativeRefs` 或若干 `Backend*` 扩展字段
- `ExtensionState` 或 backend-scoped metadata

理由:

- Claude 没有 Codex 风格 `threadId` / `turnId` / `itemId` 三件套。
- 如果继续强塞假 `threadId` / `turnId`，后面历史、resume、debug、故障定位都会变混乱。
- 如果不给 backend-specific 状态留位置，未来一旦出现 Claude-only 或 Codex-only 高级能力，又会被迫塞回共享模型里，重新走向“一个 backend 迁就另一个 backend”。

### `internal/config`

[internal/config/config.go](/home/yuhuan/feidex/internal/config/config.go) 现在只有 `[codex]` 配置块，不足以表达双后端。

推荐配置方向:

- 保留 `[codex]`
- 新增 `[claude]`
- `[[frontend]]` 的 `backend` 可以为空，表示该 frontend 启动后先不绑定 runtime，等首次消息或 `/backend` 显式选择。
- 每个 frontend 在任一时刻只绑定一个 active backend runtime；不能并发复用两个 backend。
- frontend backend 切换只能发生在该 frontend 完全空闲时。
- Feishu session 需要保留 backend-scoped thread lineage，这样 `codex -> claude -> codex` 可以恢复原来的 Codex thread。

示意:

```toml
[codex]
command = "codex"
model = "gpt-5.2-codex"

[claude]
command = "/absolute/path/to/claude"
model = "sonnet"
permission_mode = "default"
disable_plugins = false
permission_prompt_tool_stdio = true

[[frontend]]
id = "codex-main"
app_id = "cli_codex"
app_secret = "secret-1"

[[frontend]]
id = "claude-main"
backend = "claude"
app_id = "cli_claude"
app_secret = "secret-2"

[[workspace]]
id = "repo"
cwd = "/path/to/repo"
```

补充一个运维约束:

- 不要假设 daemon / systemd 的 `PATH` 等于交互式 shell。
- Claude backend 配置里最好允许显式 `command` 绝对路径，必要时允许额外环境变量覆盖。
- 这样更符合当前 Feidex daemon 的运行现实，也能减少“交互式能跑、systemd 里找不到命令”的问题。

## 历史与线程能力怎么做

`thread/read`、`thread/list` 这类能力在 Codex 是后端提供的，在 Claude 不是。

如果希望长期让 Feishu 前端真正统一，推荐方向是把“用户可见历史”从 backend-native history 迁到 Feidex 自己的 transcript/index:

- turn 期间把 neutral events 归档到本地 transcript
- history UI 优先读本地 transcript
- resume 只依赖 backend conversation handle
- list/fork 这类更强的原生线程能力，作为 backend-specific capability 暂时分开处理

这样做的好处:

- Claude 可以获得基本 history 能力
- Codex 路径也能减少对 `thread/read` 的产品层强耦合
- Feishu UI 可以在两个 backend 上保持更一致

但这件事不适合塞进第一阶段 MVP。

同时要注意边界:

- transcript/index 适合承接“共享可见历史”。
- 它不应该反过来禁止 backend 保留自己的原生历史能力。
- 如果 Codex 后续继续有 richer thread/read、fork、review context，这些依然可以作为 Codex 扩展能力存在。

## 分阶段落地建议

### Phase 1: Claude MVP

目标:

- frontend 级 backend 选择与 active runtime 绑定
- Claude 会话启动 / resume
- 基础聊天 turn
- 文本流式输出
- tool started/progress/completed
- can_use_tool 权限审批
- AskUserQuestion / ExitPlanMode
- interrupt
- usage / cost 展示
- 明确 capability gating

不做:

- review
- compact
- thread/list
- thread/fork
- thread/read
- skills/list 全量对齐
- model/list / reasoning effort 对齐

### Phase 2: 统一 transcript 和会话索引

目标:

- backend-neutral transcript store
- Claude history 基础可用
- Feidex 本地 conversation index
- session resume/card 展示更一致

### Phase 3: 高阶功能评估

逐项评估是否有稳定协议依据后再考虑:

- Claude review
- Claude compact
- richer skills surface
- 模型配置 UI 统一化
- backend extension registry 完整化
- backend-aware 菜单与命令体系

原则:

- 先有稳定 wire evidence，再有产品承诺。
- 不要根据 CLI 里“看起来存在 slash command”就直接视为可编程能力。

## 测试策略

### 1. 保住 Codex 契约

- 现有 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md) 继续作为 Codex backend 的协议合同。
- 引入 adapter 后，Codex 相关 contract tests 不能删，只能迁移到新的 adapter 边界。

### 2. backend-neutral app contract tests

新增 fake backend，验证:

- submission queue
- turn lifecycle
- approval pending / resume
- user input pending / resume
- capability gating

这样可以保证 `internal/app` 不再偷偷依赖 Codex 方法名。

### 3. Claude 协议测试

建议三层:

- parser/unit tests: 基于 `claude-cli-protocol` 的 captured messages 做解析测试
- adapter contract tests: 验证 Claude raw messages 到 neutral events 的映射
- local-only live integration tests: 手工触发真实 Claude CLI

和 Codex 一样，Claude live tests 也应该:

- 默认不进 CI
- 默认不随 `go test ./...` 运行
- 显式环境变量开启
- 单测试名手工触发

## 风险与开放问题

1. Claude protocol 目前是本地参考协议，不是 Feidex 现有产品合同的一部分，后续 CLI 漂移风险高于 Codex app-server。
2. Claude 没有 `serverRequest/resolved` 等价事件，审批恢复边界只能做 synthetic semantics。
3. `ExitPlanMode` 不提供 Codex 风格 plan delta，现有 plan 卡片交互需要降级。
4. `skills[]` 只有很轻的初始化信息，无法覆盖当前 `skills/list` 交互。
5. `model/list` 缺失意味着当前模型卡 UI 不能原样复用。
6. 如果实现时直接把 Claude raw protocol 塞进 `internal/app`，会得到一套更难维护的双重特判，而不是清晰的双后端架构。
7. 如果只实现共享 capability gate，而没有 backend extension registry，产品会自然滑向“只剩交集能力”。

## 最终建议

建议按下面的判断推进:

- 方向可做，但前提是“新增 backend adapter 边界”，不是在现有 Codex 语义上硬塞 Claude 特判。
- 第一阶段只承诺 Claude MVP 聊天能力，不承诺 Codex 全量 app-server feature parity；但这不等于把 Codex 现有高级能力降级成非一等能力。
- Codex backend 继续以状态机审计为 authoritative contract。
- Claude backend 在实现前应再补一份自己的协议映射文档，明确 synthetic event 和 capability gate。
- 实现时必须同时落下“共享核心能力”和“backend extension registry”两条线，否则最终产品形态会偏成最低公分母。
