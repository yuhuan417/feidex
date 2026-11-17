# DSH Backend Design

状态: Draft
日期: 2026-11-17

## 目标

让 Feidex 可以把 DSH Harness 作为独立 backend 使用，同时满足以下产品需求:

- 从 Feishu 启动和恢复 DSH 会话。
- 浏览由 DSH Web 等入口创建的持久化历史会话，并读取 transcript。
- 将普通输入送入队列；当 Feishu 回复对应活动 turn 时，精确 steer 到该 turn。
- 中断指定活动 turn，并以 backend 确认的终态收口本地状态。
- 在进程断线或重启后恢复会话状态、事件与 Feidex submission 的关联。
- 保留现有 Codex、Claude 行为和 backend-scoped session lineage。

本设计不把 DSH 伪装成 Codex App Server，也不要求 Feidex 复制 DSH Web UI。产品层共享 Feidex 真正需要的 conversation、submission、turn、interaction 和 capability 语义；各 backend 的 wire、生命周期与专属能力仍由各自 adapter 负责。

## 参考资料

- [DEVELOPER.md](../DEVELOPER.md)
- [Codex App Server 状态机审计](codex-app-server-state-machine-audit.md)
- [Backend Layering](backend-layering.md)
- [DSH SDK 协议](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/sdk/protocol)
- [DSH SDK JSON-RPC Server](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/sdk/server)
- [DSH SDK profile](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/bundle/sdk-app)
- [DSH Web profile](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/bundle/web-app)
- [DSH SDK client](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/sdk/client)

## 现状与约束

### DSH Web 与 SDK 是不同 profile

`npx @deepseek-ai/dsh@<version> web` 启动 Web profile；Feidex 应使用 SDK profile，通过 stdio JSON-RPC 驱动 runtime，例如 `npx @deepseek-ai/dsh@<pinned-version> --profile sdk`。两者是不同的运行时 composition，profile 插件安装目录也不同。装入 `profiles/web` 的插件不会自动进入 `profiles/sdk`；bridge 必须显式装入 SDK profile，并在安装或配置变更后重启该 runtime。

Web 和 SDK profile 都建立在 DSH base 与持久化 session store 之上。SDK 的 `session/resume` 接受持久化 `sessionId`，因此预期可恢复 Web 创建的会话，但 Feidex 必须使用相同 `DSH_HOME` 并通过 Web 会话 ID 显式定位。SDK 协议没有足够的 session catalog / transcript API，不能据此假定它能自动列出 Web 历史。跨 profile 的 ID 可见性、schema 兼容性及并发读写必须通过 DSH 实测验收。

恢复持久化会话只恢复会话上下文，不代表接管另一个 Web 进程里仍在运行的 Agent，也不恢复其 shell、browser 等外部进程。Web 的临时索引或界面状态不能当作持久化历史的权威来源。

### 标准 SDK 协议覆盖范围

DSH SDK 提供 `initialize`、`session/prompt`、`session/query`、`session/resume`、`session/wait` 和工作目录读写等方法，并推送 session event、agent status、用户问题、审批和 subagent 通知。它适合作为普通输入、持久化会话和交互请求的底座。

当前协议没有通用的活动 turn steer、指定 turn 中断、turn-scoped 终态关联、历史 session 列表与 transcript 读取，也没有逐 session close。SDK 的 `session/query` 对普通文本仍然是排队输入；某些内置命令（例如带正文的 `/plan`）使用内部 steer，不构成通用 steer API。客户端等待超时也不会取消 runtime 工作，shutdown 则关闭整个 SDK runtime。

SDK wire 文档标注尚无协议版本协商，且处于预发布阶段。Feidex 必须固定 DSH 版本；不得在生产配置使用会随发布漂移的 `@latest`。

## 推荐架构

```text
Feishu callbacks and cards
        |
internal/feishuapp + internal/application
        | typed backend-neutral operations and events
internal/runtime/dsh
        | process lifecycle, frontend scope, recovery
internal/adapter/backend/dsh
        | DSH SDK + Feidex bridge protocol mapping
internal/dshrpc
        | stdio JSON-RPC transport and wire types
DSH SDK profile + Feidex bridge plugin
        |
DSH Agent runtime and persistent session store
```

- `internal/dshrpc` 只负责 stdio JSON-RPC transport、请求/响应、通知和 bridge 扩展的 wire types。
- `internal/adapter/backend/dsh` 将 DSH session/event/interaction 载荷转换为 Feidex application 语义；不放 Feishu UI 策略。
- `internal/runtime/dsh` 负责每个 frontend 的 DSH 进程、启动探测、关闭、恢复、取消和 transport failure 收口。
- capability、backend 选择与配置沿用现有 `internal/application/backendcaps`、`internal/application/backendconfig`、`internal/feishuapp/backend_*` 和 `internal/composition` 边界。
- 产品 session/submission/turn 状态继续由 domain/application 管理；所有异步 session 变更进入对应 session actor；进程与客户端由 frontend-scoped `FrontendOwner` 构造。
- DSH 事件不能直接驱动 Feishu SDK；仍经过 semantic event/effect 和现有 Feishu adapter。

每个 Feishu frontend 持有独立 DSH runtime 和 cancellation scope，不能通过共享一个活跃 DSH agent runtime 来绕过 frontend 隔离。历史目录可以是 machine-scoped 的，但 Feidex 的 session binding、活动 turn、pending interaction、事件游标和 message link 必须 frontend-scoped。若多 runtime 共用 DSH_HOME，必须先确认 DSH store 的并发访问、锁和 SQLite journal 行为。

## Bridge 交付形态

Bridge 需要装入 `sdk` profile 的 composition。普通 DSH profile 插件是否能扩展 SDK JSON-RPC Server 的 request router、访问它实际拥有的 Agent，以及订阅/恢复所需 session 数据，尚不能仅从插件安装能力推断。实施前必须确认 DSH 提供正式的 SDK server extension hook；若没有，应先向 DSH 增加 extension provider 接口或上游协议方法。不能另起一个与 SDK Server 不共享 Agent 实例的旁路插件，再声称其 steer/interrupt 可作用于 SDK session。

建议优先把通用能力作为 DSH SDK 正式扩展协议交付；Feidex 插件提供 DSH 内部实现和 composition。若短期只能维护私有方法，使用 `feidex/*` 命名空间、独立版本字段和 capability handshake，避免与未来 SDK 标准方法冲突。标准 SDK 的 prompt、resume、approval 和 question 方法继续按其公开协议调用。

## Bridge 首期协议

所有请求都按 typed payload 定义，不暴露任意 `Call(method, params)` 到 application。

| 能力 | 建议操作 | 语义 |
| --- | --- | --- |
| 能力协商 | `feidex/initialize` 或 initialize 扩展字段 | 返回 bridge 版本、协议版本和 capabilities；缺少必需能力时启动失败，不静默降级。 |
| 历史列表 | `feidex/session/list` | 分页列出持久化会话，支持 cwd、更新时间和 archived 过滤；返回稳定 session ID、标题、摘要、cwd、创建/更新时间。必须能发现 Web profile 创建的会话。 |
| 历史读取 | `feidex/session/read` | 按游标分页读取指定 session 的持久化 transcript/event，保留角色、内容块、时间和必要的 tool/attachment 引用。历史读取不产生新的 live notifications。 |
| 会话恢复 | SDK `session/resume` | 恢复精确的持久化 session ID；不得因缺失或不兼容而新建替代会话。 |
| 当前状态 | `feidex/session/status` | 返回 session 是否运行、活动 turn ID、turn 状态和当前 event cursor，作为恢复/steer 决策的快照。 |
| 活动事件 | `feidex/events/subscribe` + cursor replay，或可重放的通知流 | 为 turn 生命周期及输出提供有序事件和单调 cursor；允许断线后从最后确认 cursor 续读。 |
| 精确 steer | `feidex/session/steer` | 参数至少含 `sessionId`、`expectedTurnId`、`submissionId` 和 content blocks。只有目标 turn 仍活动且输入已被接受时才返回 accepted。相同 submission 重试不得重复注入。 |
| 指定中断 | `feidex/session/interrupt` | 参数含 `sessionId`、`expectedTurnId`。响应只确认中断请求已受理；本地活动状态保持到 backend 的 interrupted/failed/completed 终态事件。 |
| 关闭 Agent | `feidex/session/close` | 释放该进程中的 Agent/子进程资源但保留持久化 session 历史；不能等价为删除会话或删除 transcript。 |

事件至少需要表达：turn accepted/started、assistant 输出、必要的 tool/interaction 生命周期、turn completed/failed/interrupted 和 backend error。事件都绑定 `sessionId`、稳定 `turnId`、可选 `submissionId`、cursor 与发生时间。完整文本和附件引用仍需保留，不应只提供供卡片渲染的摘要。

审批和 user question 首期直接复用 DSH SDK 的 `approval/request`、`user-question/ask` 回调；adapter 负责转成 Feidex interaction。若 Feidex 没有注册 responder，或 DSH SDK 不能可靠取消/终结待决请求，必须 fail closed 并按 DSH 终态清理，不得把超时当作批准或已解决。

## 历史会话语义

- 列表和 transcript 以 DSH 持久化 store 为权威，不从 Web profile 的内存态、浏览器缓存或仅供全文搜索的索引拼装结果。
- `session/list` 与 `session/read` 的分页游标须稳定定义；会话更新期间要么提供一致快照，要么通过 cursor 明确新增事件，不能静默跳项或重复事件。
- 读取范围至少支持当前 workspace，并明确是否允许跨 workspace 浏览。恢复时必须验证保存的 cwd 与当前 workspace 策略；不能静默把旧会话改绑到另一个 workspace。
- transcript 记录仍属于其原始 DSH session。Feidex 不复制并改写 DSH 历史来制造兼容性；Feishu 展示转换放在 adapter / Feishu presentation 层。
- Web 会话可能包含当前 Feidex 前端不支持的事件类型。未知记录必须可跳过或作为受控的 unsupported item 展示，不能破坏整页读取。
- Web 活动 Agent 的状态与 SDK runtime 的 Agent 状态是不同进程的事实。历史列表可见不代表同一 session 可同时被 Web 与 Feidex 写入；恢复和写入必须有 DSH 侧占用/并发策略。

## Turn 与 steer 状态机

1. Feidex 保存 queued submission 与活动 turn 绑定，再向 bridge 发送 steer，携带当前 `expectedTurnId` 和稳定 `submissionId`。
2. Bridge 原子验证活动 turn 后接纳输入并返回接受回执；拒绝或过期 turn 不得伪装成排队成功。Feidex 只有在回执明确 accepted 后才把 submission 记录为 steered。
3. DSH 事件携带目标 turn ID 与 submission ID。turn 更替时，旧 turn 的晚到事件不能结束新 turn 或释放新 turn 的 active operations。
4. interrupt 操作仅发出请求；最终状态由有权威性的终态事件决定。transport loss 是异常收口路径，需通过 `session/status` 和 cursor replay 对账后再决定本地状态。
5. event cursor 持久化在 frontend-scoped runtime state。恢复先读取状态快照，再从确认 cursor replay；重复事件按稳定 event ID/cursor 幂等处理。
6. 对同一 DSH session 的命令、事件和 Feishu 输入经 session actor 串行化，避免 steer 与 turn completion/interrupt 竞态。

Codex 协议仍单独遵守状态机审计：`SM-05` 要求 steer 对准 `expectedTurnId`；`SM-06` 要求 interrupt 请求不提前收口，等待 `turn/completed(interrupted)`。本设计将这两个产品边界应用到 backend-neutral turn 语义，但不修改或替代 Codex wire contract。DSH 的具体事件名和生命周期须由 DSH 自己的协议/测试定义。

## Capability 与产品表面

首期 DSH capabilities：

- conversation start/resume/list/read
- queued input、streaming event、turn lifecycle
- precise steer、interrupt、status/recovery
- tool visibility、approval、user question
- usage/cost（仅当 DSH 能提供可信字段；缺少时显式标记 unavailable）

首期不宣称与 Codex 等价的 `review/start`、Codex skills catalog、model catalog、thread fork、Codex compaction 或 Codex server-request 类型。菜单、命令和卡片动作必须由 capability 显式控制，并保留 backend-specific 能力为一等扩展。DSH 自有能力后续可以加入 DSH extension，而不要求 Codex/Claude 同时实现。

历史会话浏览属于 DSH 首期必需能力，不能因为 SDK 本身没有 session catalog 而降级为仅能恢复 Feidex 已知的 session ID。

## 配置与运行

- 增加 backend kind `dsh` 和明确的 executable/profile 配置；默认命令不得隐式使用 `@latest`。
- 典型运行入口为固定版本的 `dsh --profile sdk`，由 Feidex 管理 stdio stdin/stdout；stdout 只能有 JSON-RPC frame，诊断从 stderr 读取。
- 在 SDK profile 的 `profiles/sdk` 安装 bridge；Web profile 的 `profiles/web` 插件配置不会自动影响 SDK。
- 同一个 DSH_HOME 是跨 Web/SDK 历史可见的必要条件之一；profile 配置仍然独立。SDK profile 的 provider/model、tools、permissions 与 Feidex 配置需定义明确的配置 owner，避免启动参数与 DSH 持久化选择冲突。
- 后端启动探测超时不能变成进程生命周期；shutdown 先取消 frontend context，再按有界期限关闭 JSON-RPC/进程并收敛已接收工作。
- DSH 切换仍遵守 frontend idle-only：无活动 turn、队列/staged input 或待处理 interaction。切回 DSH 时恢复该 frontend 绑定的 DSH session lineage。

## 分阶段实施

### Phase 0: 上游接口与数据兼容验证

- 固定目标 DSH 版本并检查其 SDK wire schema、profile composition 和 server extension hook。
- 在 Web profile 创建测试会话，在相同 DSH_HOME 下通过 SDK profile 列出、读取、resume；覆盖 SDK 重启和 DSH 版本升级/不兼容场景。
- 验证 Feidex bridge 能访问 SDK server 拥有的准确 Agent、turn 和持久化 store。
- 验证并发读写、session 占用、interrupt、每 session close、事件 replay/cursor 和待决 approval/question 的 teardown。
- 若没有可靠的 SDK server extension hook 或 Web 会话不能按稳定 ID 读取，则先做 DSH 上游支持；不开始 Feidex backend 主链路实现。

### Phase 1: DSH process 与标准 SDK 核心

- 实现 `internal/dshrpc`、`internal/adapter/backend/dsh`、`internal/runtime/dsh` 和 composition/config 注册。
- 接入初始化、普通 prompt、基础 session event、approval/question，以及必需的 `session/list`、`session/read`、resume 和 Feishu 历史浏览。
- 完成 frontend/session 隔离、启动失败、transport failure 与 shutdown 行为。

### Phase 2: turn control bridge

- 接入可 replay turn event、状态快照、幂等 steer、指定 turn interrupt 和每 session close。
- 验证 stale turn、重复 submission、transport loss、晚到 completion、进程重启和 interrupt 竞态。
- 只有在 bridge 契约测试通过后，才把 Feishu 回复当前任务的路径对 DSH 开放为 steer。

### Phase 3: parity 与专属能力

- 根据 DSH 实际能力逐项增加 usage、workspace 工具、历史搜索或 DSH 专属产品能力。
- 每个能力同时更新 capability declarations、直接命令入口、菜单/card 与测试；不通过静默 fallback 到 Codex 实现。

## 测试与验收

自动化契约测试覆盖：

- 对同一个 DSH_HOME，Web 创建的 session 可被 SDK bridge 列表发现、按游标读取和 resume。
- 缺失 session、坏游标、workspace 不匹配、schema 不兼容均显式返回错误，不创建空替代会话。
- event replay 在断线重连后不丢不重；消费重复事件不重复结束 submission 或 interaction。
- steer 要求匹配活动 turn，使用相同 submissionID 重试不重复注入，过期 turn 拒绝。
- interrupt 被接受后本地仍保持 in-flight，直到对应 turn 的 terminal event 到达。
- session close 释放 runtime 资源但不删除历史，之后可 resume。
- SDK shutdown、EOF、启动失败与 profile 插件缺失能及时释放进程和等待中的 RPC。
- approval/question 使用 DSH 标准 SDK request-response；传输断开和取消不能导致 fail-open。
- 多 frontend、共享 DSH_HOME 的并发读写与同 session 占用策略符合隔离要求。

真实模型/凭据测试保持 opt-in、手动运行；默认测试使用 fake JSON-RPC peer 和可复现的本地 DSH fixture，不运行 token-consuming turn。

## 开放问题

1. DSH SDK Server 是否有稳定的插件扩展点，可把 handler 注册到同一个 stdio JSON-RPC dispatcher 并获得所拥有 Agent 的控制能力？没有时需要上游 API 变更。
2. Web 与 SDK 实际共用的持久化 session store/API 是什么？能否稳定枚举、读取历史 session event，并支持 cursor pagination？
3. DSH 是否支持多个独立 SDK process 并发打开同一 DSH_HOME；写入锁、journal 和 session lease 的语义是什么？
4. Web 正在运行的 session 能否由另一个 SDK runtime resume，还是必须先检测并拒绝占用？
5. DSH 的 interrupt、steer 和 close 原语是否为 Agent 官方生命周期 API，能否对并发 tool/approval/question 做有界取消？
6. Web 会话的权限、provider/model、workspace 与附件引用在 SDK resume 时如何保留和校验？
7. DSH 预发布协议没有版本协商；Feidex bridge 与 SDK runtime 的兼容版本范围如何声明和验证？

以上问题必须在 Phase 0 形成可重复的集成测试结论；不能以“本机手工试过一次”作为跨 profile 兼容承诺。

## 非目标

- 不在 `internal/app` 引入 DSH JSON-RPC 方法名或 DSH 原始 payload。
- 不让 DSH backend 模拟 Codex App Server，也不改变 Codex 状态机审计合同。
- 不让 DSH bridge 成为另一个 Feishu bot 或绕开现有 application/effect/actor 边界。
- 不把读取 Web 历史等价成接管 Web 进程里的活动任务。
- 不默认提供任意 profile patch、插件安装或 DSH 全局配置管理的 Feishu UI。
- 不使用浮动的 `npx @deepseek-ai/dsh@latest` 作为生产启动策略。
