# 拆掉 `feishuapp` 的 `*App` 聚合

本文是施工图，记录把 `internal/feishuapp` 的 `*App` 引用清掉的路径、已完成的
步骤，以及下一步该改什么、为什么按这个顺序。

构造期存在两个真环，必须先拆掉才能让快照式的能力包适用于所有工厂 —— 见
[拆掉 feishuapp 的构造环](feishuapp-construction-cycle-breaking.md)。

## 为什么 `Bindings` 存在

`internal/composition/app.go` 是两阶段构造 + 反向引用：

```go
bindings := &feishuapp.Bindings{ /* 一部分字段 */ }
frontend.AttachBindings(bindings)                       // 挂到 App 上
bindings.CardActions = ...CardActionPorts(frontend)     // 之后才填剩下的
```

后一半之所以能读 `frontend`，是因为 `bindings` 已经挂上去了。**App 需要
Bindings，Bindings 需要 App**，104 字段的服务定位器就是这个环的产物。

所以"把 `*App` 参数换成窄接口"单独做是没有强制力的：`*App` 结构化地满足
任何由它自己的方法组成的接口，调用方继续传 `*App` 照样编译，运行时流进去
的还是聚合对象。真正让聚合停止流动的动作是**拆掉 `Bindings`**：让每个
`*Ports` 工厂从参数拿服务，composition 传它手里已有的那些。

## 当前状态（2026-10-04）

| 指标 | 值 |
|---|---|
| `internal/feishuapp` 生产代码里的 `*App` 引用 | 482 |
| 收 `*App` 的顶层函数 | 294 |
| 收 `*App` 的 `*Ports` 工厂 | 16 |
| **持有 `*App` 字段的结构体** | **58** |

棘轮只有一个方向：任何一次提交都不许让这些数字变大。惰性读取另有单独的
预算（`TestFeishuAppLazyBindingReadsDoesNotGrow`，见构造环文档），当前 34。

单成员 helper 的转换有个副作用值得记住：把 `f(a)` 改成 `f(a.bindings.X)` 时，
如果调用点本身在闭包里，惰性读取预算会**上涨**——读取从 `f` 的函数体（不算惰性）
搬进了闭包。语义没变（闭包体里的表达式仍是运行时求值），只是度量口径看见了它。
处理办法是把那处读取提升成构造期局部变量，两边一起降。

## 施工顺序（2026-10 修正版）

**先前的施工图是错的**：它只统计工厂函数体内**直接调用**的 `*App` helper，
于是 `CardActionPorts` 显示"零依赖"，而它实际把一个 `*App` 结构体交了出去。
真实形态是：**依赖藏在工厂交出去的结构体的方法里**。

`scripts/depmap` 现在沿三条边求传递闭包：

```
工厂 --调用--> 收 *App 的 helper
工厂 --实例化--> 字段为 *App 的结构体
结构体方法 --调用--> 收 *App 的 helper
```

### 当前工厂排序（2026-10-04）

以下计数由 `go run . <repo>/internal/feishuapp --json` 生成：direct 是工厂直接
读取的不同 `App` 成员数，helpers 是直接调用的收 `*App` 函数数，structs 是工厂
实例化的持有 `*App` 字段结构体数。它们是依赖图规模指标，不代表改动成本。

当前扇入最高的 App-bearing structures：

| 工厂数 | 结构体 | 方法数 | helper 数 |
|---|---|---|---|
| 3 | `outboundCardService` | 7 | 12 |
| 3 | `sqLiveThreadAdapter` | 3 | 3 |
| 2 | `menuActionService` | 21 | 15 |
| 2 | `planModeOutbound` | 4 | 4 |
| 2 | `goalOutbound` | 3 | 3 |
| 2 | `turnRuntimePort` | 3 | 2 |
| 2 | `planModeCardRenderer` | 1 | 0 |

`cardRenderer` 已不再持有 `*App`，不属于这份图。当前工厂按直接依赖总数排序：

| 合计 | 工厂 | direct | helpers | structs |
|---|---|---|---|---|
| 3 | `CardActionPorts` | 2 | 0 | 1 |
| 3 | `BackendSwitchPorts` | 2 | 0 | 1 |
| 5 | `ConversationControlPorts` | 4 | 0 | 1 |
| 5 | `FileSharePorts` | 2 | 2 | 1 |
| 5 | `ReviewPorts` | 1 | 1 | 3 |
| 6 | `BackendFailurePorts` | 3 | 3 | 0 |
| 6 | `GoalContinuationPorts` | 4 | 0 | 2 |
| 6 | `TurnPresentationPorts` | 3 | 1 | 2 |
| 7 | `ConversationPorts` | 6 | 0 | 1 |
| 7 | `TurnPorts` | 2 | 3 | 2 |
| 9 | `CodexRecoveryPorts` | 5 | 4 | 0 |
| 9 | `StartupRecoveryPorts` | 6 | 3 | 0 |
| 10 | `AutoRetryPorts` | 7 | 2 | 1 |
| 12 | `InboundPorts` | 5 | 2 | 5 |
| 16 | `ClaudeRuntimePorts` | 6 | 9 | 1 |
| 19 | `SubmissionPorts` | 7 | 9 | 3 |

## 已完成的骨架

`BackendRuntimeDeps` 是已经落地的能力包：导出类型、字段不导出，由
`App.BackendRuntimeDeps()` 构造。composition 拿到它传给工厂，`*App` 的捕获
被关在这一个方法里。后续的包照此办理。

已完成的两条能力视图：`frontendConfigView`（配置 + 锁 + frontend 身份）、
`runtimeView`（runtime owner 及其后端客户端）。

## 已完成

| 步骤 | 工厂 | 改成了什么 |
|---|---|---|
| 1 | `InteractionPorts` | `(store *appstate.Store, submissionLookup appsubmission.SubmissionLookupService)` |
| 2 | `BindingReplayPorts` | `(actors *runtime.SessionActors, runtimeOwner *runtime.FrontendOwner)` |
| 3 | `BuildWorkspaceConfiguration` / `BuildWorkspaceManagement` | 加了 `(*workspacecards.Presentation, *conversationapp.Service)` 两个构造期参数，26 处惰性读取改为构造期读取 |
| 4 | `BackendFailurePorts` | 9 处 `a.bindings.X` 改为函数开头的局部变量（快照式） |
| 5 | `buildBackendConfigurationService` | 删掉死代码权限链后，`ModelCommands` 改为构造期读取 |
| 6 | 18 个"只用一个成员"的 helper | 单成员函数改收那个成员（store、tracker、lookup、query、service、client），不再收聚合 |
| 7 | `PlanPorts` / `planSettingsSource` | 显式接收配置、配置锁、模型快照服务和 runtime owner；返回对象不再持有 `App`，生产 composition 与测试 fixture 使用同一组输入 |
| 8 | `CompactionPorts` | 显式接收 context、scoped store、runtime owner、frontend ID 与通知开关；client 查询和通知闭包不再捕获 `App` |
| 9 | `ContinuationPorts` / `resolveInboundAttachments` | 显式接收配置、配置锁、context、scoped store、runtime owner、submission queue、frontend 身份与 Feishu client；状态方法值和附件下载不再捕获 `App` |
| 10 | `ClaudeMaintenancePorts` | 显式接收配置、配置锁、context、runtime owner、frontend 配置索引与 Claude core factory；运行时选择与配置仍动态读取 |
| 11 | `ConversationRecoveryPorts` | 显式接收 scoped repository、conversation service、配置视图、runtime owner、Codex recovery 与 conversation configuration；恢复 endpoint 仍捕获当前 client 并校验其有效性 |
| 12 | `CodexUpgradePorts` | 显式接收配置、配置锁、frontend 身份、runtime owner、runtime dependency snapshot、Codex recovery 与 startup recovery；配置和 backend 仍动态读取 |
| 13 | `BackendMaintenancePorts` | 改为显式接收配置/锁、maintenance state、Codex upgrade 与 Claude maintenance owner、渲染和 patch ports；runtime callback 仍观察 Claude service 的更新字段 |
| 14 | `GoalCommandPorts` | 移除 App-bearing 工厂与 outbound/renderer；composition 显式组装 `goalcmd.Dependencies`，outbound adapter 仅持有 frontend ID 与 effect runner |
| 15 | `BackendEventPorts` | 移除 App-bearing presenter 与 ports 工厂；composition 直接连接 backendevents owners，并为 interaction presenter 注入 submission/workspace/item-context/server-request/Codex error capabilities |
| 16（分阶段） | `CardActionPorts` | server-request actions 的 owner 参数显式化；当时仍经统一 callback wrapper 捕获 App，捕获边界在步骤 19 修正 |
| 17（分阶段） | `CardActionPorts` | review form 与 Claude plan-approve actions 的 owners 显式化；当时仍经统一 callback wrapper 捕获 App，捕获边界在步骤 19 修正 |
| 18（分阶段） | `CardActionPorts` | upgrade/restart actions 的 owners 显式化；当时仍经统一 callback wrapper 捕获 App，捕获边界在步骤 19 修正 |
| 19 | `CardActionPorts` | 将 owner-only handlers 与 App handlers 分开绑定；前者的 handler 类型和回调闭包均不接收或捕获 App |
| 20（分阶段） | `CardActionPorts` | normalization 与 backend-switch callbacks 改为 composition 传入的窄依赖；Feishu callback dispatcher 与 App handler context 分型，剩余 App-handler families 仍待拆分 |
| 21 | card-action dispatcher | dispatcher 构造直接接收已组合的 `cardaction.Service`；nil action 直接返回空响应，移除生产 `newCardActionService(*App)` accessor |

在最初纳入分析的 29 个工厂中，前两个是仅有的**立即求值、不捕获**工厂；当时
步骤 3-5 也沿用这条路径：值在调用时已经就绪，惰性读取纯属写法惯性。

步骤 7 保留动态配置读取与当前 frontend 的 Codex client 查询，不把构造期 client
冻结进 catalog。`plan_ports_test.go` 覆盖配置更新、session 模型覆盖、workspace 更新、
client 替换与移除。对照 SM-04/05：Plan 配置仍在本地 turn 启动时捕获，steer 仍沿用
原 turn；本次只改变依赖传递方式。惰性读取预算保持 38。

步骤 8 保留当前 frontend 的动态 Codex client 查询，通知继续通过 runtime effect
runner 投递到 session 的 chat，无 Feishu 或 chat ID 时跳过通知。
`compact_ports_test.go` 覆盖 client 替换与移除、effect 的 frontend/chat 身份、
operation context 与取消。对照 SM-08：compact 的 started/item/completed 绑定和
终态保持不变；构造期环与反向 eager 读取仍为 0，惰性读取预算保持 38。

步骤 9 保留运行时 backend 选择与 Codex client 的动态读取，配置回退仍限定到当前
frontend entry。状态存储继续使用 scoped store，附件下载保留 forwarded message ID
优先级和 30 秒 timeout；Claude continuation 仍走原 submission queue 的 steer 路径。
`continuation_ports_test.go` 覆盖 backend/配置更新、frontend 隔离、client 替换与移除、
`expectedTurnId` 和附件来源。对照 SM-04/05：不改 turn 绑定、终态、模型快照与 steer
语义；惰性读取预算保持 38。

步骤 10 保留维护 smoke 使用的前端生命周期 context、当前 Claude 配置快照以及
workspace workdir 选择；active/current/create 继续查询 runtime owner，create 每次
从配置锁下读取最新 Claude 配置后调用注入的 factory。`claude_maintenance_ports_test.go`
覆盖 backend 切换、core 替换和配置更新。维护流程的 smoke → active 检查 → runtime
刷新顺序不变；惰性读取预算由 38 降至 37。

步骤 11 保留 Claude lazy resume 和 Codex 捕获当前 client 的恢复端点；Codex endpoint
只有在 transport 未处于 recovery 且 client 仍是 frontend 当前 client 时才有效。
`conversation_recovery_ports_test.go` 覆盖 backend 选择、恢复中的状态、client 替换和
Codex `thread/resume` gateway。对照 SM-03：启动恢复仍由现有 Conversation Recovery
用例按原顺序执行 resume、失败后的 fresh start 与状态绑定；本次仅显式化 owner 注入。
惰性读取预算保持 37。

步骤 12 先构造 Codex recovery，再构造 upgrade ports，并把已就绪的 recovery
service 放入 `BackendRuntimeDeps`。恢复入口与 smoke test 使用 composition 局部的
upgrade service 值完成回调连接；ports 本身不再读取 `App` 或 `Bindings`。配置读取
在配置锁下取快照，backend、当前 client 和 recovery state 仍按 frontend owner 动态
读取。`codex_upgrade_ports_test.go` 覆盖配置更新、backend 切换、client 替换、
transport handler 注入和 startup recovery 回调。对照 SM-03：没有改变 startup
recovery 中的 thread resume、失败后的 fresh start 或状态绑定顺序；惰性读取预算由
37 降至 36。

步骤 13 将两种 maintenance runtime/publisher 从捕获 `App` 的 struct 改为窄依赖，
并把 backend maintenance map 的构造移到 Codex/Claude runtime service 就绪之后。配置命令
仍在调用 installer 时从配置锁下读取；Claude runtime adapter 持有 service pointer，
每次执行时读取最新 smoke/config callbacks；renderer 和 patcher 只捕获局部 transport、
frontend identity 与 effect runner。`backend_maintenance_ports_test.go` 覆盖配置更新、
runtime callback、operation card 渲染与 patch 路径；Claude upgrade/restart 失败时旧
runtime 保持打开的既有用例也通过。没有改变维护 operation 的开始、验证、切换或失败
收口顺序；惰性读取预算由 36 降至 34。

步骤 14 把 goal command 的 state、tracker、renderer、session key、菜单工具和 lifecycle
context 作为显式依赖交给 `goalcmd.NewService`。共享 outbound 同时供 goal continuation
使用，改成 frontend-scoped effect runner adapter，移除了 `goalOutbound` 与
`goalCardRenderer` 上的 `*App` 字段。通用菜单命令 callback 仍进入现有 Feishu command
dispatcher，因此这条 callback 目前仍由 composition 绑定到 `App`；goal tracker、Codex
gateway 与 continuation 生命周期没有变化。`goal_command_ports_test.go` 覆盖 effect 的
frontend/消息身份和 session key 兼容格式；Goal command、feishuapp 与 composition 测试通过。
`*App` 引用预算由 509 降至 506，惰性读取预算保持 34。

步骤 15 将 `BackendEventPorts` 的 bindings 读取移入 composition 的显式
`backendevents.Dependencies` 组装，并把 interaction presenter 的事件投影改为窄 ports。
workspace cwd 经 `WorkspaceRepository` 在事件处理时查询，started item 上下文仍通过
`ItemContext.MergePresentation` 合并，request error 始终回到 runtime owner 当前 Codex
client。对照 SM-09/10/11/22/23：approval、user-input、elicitation、rejected 的分支和
request token/error code 均不变；turn lifecycle、item、compaction、usage、goal 与
resolved/resume owner 继续由原 service 处理。`backend_events_ports_test.go` 覆盖上述
interaction 分支。`*App` 引用预算由 506 降至 503，惰性读取预算保持 34。

步骤 16 先拆 server-request handler family：工具输入、命令/文件/permissions approval、
MCP elicitation form/url 的 callback handler 直接绑定 composition 提供的 `ServerRequests`
service，减少 handler 对 `App` binding accessor 的依赖。CardActions 的构造相应移到
composition 和 fixture 的后段，保证 owner 已就绪。handler 仍委托原 `serverrequest.Service`，
不改变 payload、pending 状态写入或 reply/resolved 边界；对照 SM-09/10/11/22/23。handler
名称唯一性与 callback 路由用例通过。`CardActionPorts` 的其他配置、backend-switch 与 handler
依赖仍在，因此此步骤是 family 级拆分，不代表工厂已解耦；`*App` 与惰性读取预算分别保持
503、34。

步骤 17 继续将 `review.base.select`、`review.commit.select`、`review.form.submit` 与
`pending_form.plan_approve` 从通用 callback 上下文移出，分别注入 `ReviewCommands` 和
`ClaudeSupport` owner。composition 在 review commands 构造完成后创建 CardActions；处理逻辑
仍委托现有 owner，action names 与返回行为不变。该阶段其余 pending action、菜单/workspace/
maintenance handlers 及 callback policy 仍保留 App 依赖；后续步骤继续拆分这些依赖，预算保持 503/34。

步骤 18 将 upgrade confirm/cancel/local-pick 与 Codex/Claude upgrade/restart callbacks
的 owner 参数改为 `Upgrades`、`BackendUpgrades`；`upgrade.dev` 仍经 menu action helper
处理。操作仍由原 upgrade services 执行，callback 的确认和异步维护边界不变。

复核发现步骤 16-18 虽然显式传入了 service owners，但当时所有 handler 共用的绑定闭包仍
捕获 `App` 并构造 `cardActionService`，所以这些 action 还没有实现运行时的 App 解耦。步骤 19
将 handler 分为两种类型：需要 App helper 的 handler 接收 `cardActionService`，owner-only
handler 只接收 `*feishu.CardAction`。两类 handler 使用独立 binder；owner-only binder 的闭包
只捕获其 handler/owner，不引用 App。步骤 20 再将 session-key normalizer 改为只捕获 frontend ID
的纯函数，将 backend-switch guard 直接绑定 runtime transition owner，并把只持有 application
card-action service 的 `cardActionDispatcher` 与持有 App 的 handler context 分型。因此 owner-only
callbacks 的完整调度路径不再持有 App。菜单、workspace、`upgrade.dev` 与本地 pending handlers
仍使用 App helper；预算保持 503/34。

步骤 21 移除生产 `newCardActionService(app *App)`。composition 的 input dispatcher 直接把
已组合的 `bindings.CardActions` 放进 `cardActionDispatcher`；nil action 快速返回空响应，不再
为了这个边界对象绕回 App。`*App` 引用预算由 503 降至 502，惰性读取预算保持 34。

步骤 22 将 `groupBindingScopeActive` 改为只接收 inbound message。该 helper 只根据
`ChatType`/`ChatID` 判断是否为群消息，原 `*App` 参数未被使用；四个 workspace/model/fast
命令调用点同步移除该实参。未改变群绑定命令与单聊 profile-aware 命令的分流。架构预算由
502 降至 501；此改动不触及 app-server lifecycle 或 pending request 状态。

步骤 23 从 `completeMaintenanceRestartRun` 移除未使用的 `*App` 参数，并同步唯一调用点。
该 helper 仍按原有参数把 begin/run 与操作卡、状态卡、失败卡 renderer 委托给
`appmaintenance.CompleteRestartRun`；restart 执行及异步维护边界不变。架构预算由 501 降至
500；不涉及 app-server turn 或 pending request 状态转换。

步骤 24 将 `replyInThreadForSubmission` 改为只接收 submission。原函数把 `*App` 与
submission 都标为未使用并恒返回 `false`；所有 turn、delivery、MCP、Plan 与 Claude
streaming 调用点不再把 App 传入该决策函数。线程回复策略和调用路径结果不变。架构预算由
500 降至 499；不涉及 app-server lifecycle 或 request 状态。

步骤 25 从 `ConfigService` 中抽出 `WorkspaceDeleteActions`，只持有 workspace
workflow 与三项 workspace-card presentation 函数；该对象不保留 `ConfigService`，而
presentation 函数只捕获 composition 已构造的 `workspacecards.Presentation`。原 ConfigService
的删除 prompt/confirm API 委托同一个实现，CardAction composition 则直接注入窄 owner，删除
确认不再经过 `cardActionService{app: ...}`。删除校验、workflow、toast、卡片和目录保留语义不变；
删除菜单仍走 App handler，以保留群聊禁止删除本机 workspace 的 guard。此路径解耦不改变词面
`*App` 引用数，预算保持 499；架构测试与 action dispatcher 集成测试覆盖现有分支。

步骤 26 将 history 构造从 `BuildHistory(*App)` 改为显式接收 frontend identity、scoped
repository、动态 runtime backend/Codex client/context、effect runner 与 frontend session-key
builder。history outbound 只持有 identity/effect runner；分页、详情和选择回调绑定
`history.Service` owner，移除 menuActionService 中重复的 App-bound history handlers。
backend、Codex client、lifecycle context 和 session key 仍动态取自对应的 runtime/config
owners。history 查询与卡片行为不变。生产 `*App` 引用预算由 499 降至 497，app-bearing
函数数降至 302；不涉及 turn lifecycle 或 pending request 状态。

步骤 27 将 `serviceTierOutbound` 改为持有 frontend identity 与 effect runner，并由
composition 注入 context、现成的 `ThreadSettings` owner 和 session-key builder。回复卡片与
文本仍经同一 effect runner 使用原 message anchor 和 thread 标志发送。生产 `*App` 引用预算
由 497 降至 495，app-bearing 函数数降至 301，持有 App 字段的结构体降至 63；不涉及
Codex turn 或 pending request 状态。

步骤 28 将 `pendingCardPresenter` 改为显式持有 frontend identity、deduper、turn-stream
service 和 effect runner。交互卡仍先尝试复用 reasoning-only 工作卡，再回复到原触发消息，
最后发送到 session chat；pending 状态写入、resolved 边界与 message-link 持久化仍由原
`InteractionDelivery` service 完成。`*App` 引用预算由 495 降至 494，持有 App 字段的结构体
降至 62。对照 SM-09/10/11/22/23/26，未改 request reply/resolved 顺序和 turn 生命周期。

步骤 29 将 `anchorForSubmission` 改为仅从 submission 投影 pending-card anchor，去掉未使用的
`*App` 参数，并同步 pending delivery 与 message-link 调用点。投影字段和回复策略均不变；
`*App` 引用预算由 494 降至 493，app-bearing 函数数由 301 降至 300。

步骤 30 删除 `menuCardBodyForSession` 与 `menuCardBodyForBackendForSession` 两个纯转发包装，
在调用点直接使用 backend-aware 或普通菜单 body renderer。两个包装原本没有读取 `App` 或
session key，菜单文案、backend 选择与 action breadcrumb 均不变。生产 `*App` 引用预算由
493 降至 491，app-bearing 函数数由 300 降至 298。

步骤 31 将 `canonicalSessionKeyForApp` 改为只接收 session key 的纯转换函数，并移除
`sessionKeysEqual` 中未使用的 App 参数；server-request 的 session 匹配改为直接调用纯比较。
规范化及 auxiliary session-key 保留规则不变。生产 `*App` 引用预算由 491 降至 489，
app-bearing 函数数由 298 降至 296。

步骤 32 将原 `forwardPorts` 拆为 `forwardGatewayAdapter` 与 `forwardTaskAdapter`：前者只持有
单方法 merge-forward Feishu capability，后者只持有 frontend lifecycle 与 async executor。
composition 和 fixture 传入已持有的 transport/runtime owner，ForwardService 的超时、排队、
失败与 shutdown admission 行为不变。生产 `*App` 引用预算由 489 降至 486，app-bearing
函数数由 296 降至 294，持有 App 字段的结构体由 62 降至 61。

步骤 33 将 `sqLiveThreadAdapter` 改为持有 runtime tracker、session lookup、announcement
group query 和 refresh scheduler；构造处从 composition 显式传入这些 owner，announcement
coalescer 的创建相应提前到 SubmissionPorts 之前。session/thread 校验、tracker 标记/清理和
群 session live 时的公告刷新均保留。对照 SM-04，未更改 turn 启动/started/completed 顺序；
`*App` 引用预算由 486 降至 485，持有 App 字段的结构体由 61 降至 60。

步骤 34 将 `workspaceEffectRuntime` 改为显式持有 frontend lifecycle、异步 executor、session
actors、live-thread tracker 和 BindingReplay。composition 与 fixture 在构造 WorkspaceEffects 前
先完成 BindingReplay，再把依赖直接传入；ClearLive、session actor 串行化、生命周期 admission
和 replay 行为保持不变。对照 SM-03/SM-04，不更改 thread/turn 协议状态和 turn 事件顺序；
生产 `*App` 引用预算由 485 降至 483，持有 App 字段的结构体由 60 降至 59。

步骤 35 将 `debugArtifactSharer` 从持有 `App` 改为持有 Feishu `ShareLocalFile` 能力；
`FileSharePorts` 仅在构造边界提取对应 client，nil client 仍返回 `context.Canceled`。文件名、
URL 和大小的映射及上传错误透传不变。生产 `*App` 引用预算由 483 降至 482，持有 App 字段
的结构体由 59 降至 58。

## 方法

- 一次一个工厂，一个提交，随改随验：`go build ./...`、`go vet ./...`、
  `staticcheck ./...`（必须 0）、`go test -count=1 ./...`。
- composition 调用点用**它当时已经持有**的局部值（`scope.RuntimeOwner`、
  `frontend.State()`、`bindings.X`），不要为了传参而提前构造。
- 闭包捕获改成窄值后，闭包内调用的 helper 也必须已经改完 —— 这就是第一
  阶段要先做 helper 的原因。

## 重新生成施工图

两个分析器在会话外重建即可：

- `scripts/apprewrite`（已入库）—— 批量改写 `*App` 参数
- 扇入/传递依赖分析（本文件的数据来源）用 `go/ast` 遍历
  `internal/feishuapp/*.go`：收集每个收 `*App` 的函数、它的 `a.X` /
  `a.bindings.Y` 访问、以及它调用的其他收 `*App` 的同包函数，然后对每个
  `*Ports` 工厂求传递闭包并按扇入排序。

不要用正则统计这些数字：本轮曾因此得出"闭包捕获 0 次"的错误结论，而
`ClaudeMaintenancePorts` 的四个闭包实际全部捕获了 `a`。
