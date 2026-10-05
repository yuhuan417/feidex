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

## 当前状态（2026-10-05）

| 指标 | 值 |
|---|---|
| `internal/feishuapp` 生产代码里的 `*App` 引用 | 191 |
| 收 `*App` 的顶层函数 | 70 |
| 收 `*App` 的 `*Ports` 工厂 | 1 |
| **持有 `*App` 字段的结构体** | **1** |

棘轮只有一个方向：任何一次提交都不许让这些数字变大。惰性读取另有单独的
预算（`TestFeishuAppLazyBindingReadsDoesNotGrow`，见构造环文档），当前 0。`*App`
引用与 lazy binding-read 都只允许单向下降；即使某一步只改善其中一项，也不能让另一项回升。

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

### 当前工厂排序（2026-10-05）

以下计数由 `go run . <repo>/internal/feishuapp --json` 生成：`a.X` 是工厂直接读取的
非 bindings 成员数，bindings 是直接读取的 `a.bindings.Y` 字段数，helpers 是直接调用的
收 `*App` 函数数，structs 是依赖闭包中持有 `*App` 字段的结构体数。合计是这四类依赖边的
总数，用来排序而不代表改动成本。

当前扇入最高的 App-bearing structures：

| 工厂数 | 结构体 | 方法数 | helper 数 |
|---|---|---|---|
| 1 | `bindingService` | 39 | 7 |
| 1 | `cardActionService` | 0 | 0 |

`cardRenderer`、`outboundCardService`、`turnStreamOutboundCardAdapter` 和 `turnRuntimePort`
已不再持有 `*App`，不属于这份图。当前工厂按直接依赖总数排序：

| 合计 | 工厂 | `a.X` | bindings | helpers | structs |
|---|---|---|---|---|---|
| 1 | `CardActionPorts` | 0 | 0 | 0 | 1 |

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

步骤 36 将 auto-retry、debug、maintenance、plan、upgrade、workspace 与 review 的同形
`SimpleStatusCard` renderer 合并为共享的 Feishu capability adapter。七处构造分别注入现有
Feishu client，空 client 仍返回 nil，卡片调用参数和结果不变。生产 `*App` 引用预算由 482
降至 475，持有 App 字段的结构体由 58 降至 51。

步骤 37 将 autoretry、debug、maintenance、plan、review、upgrade、workspace、skills 和
thread-menu outbound wrappers 合并为共享 `effectOutbound`。adapter 仅持有 frontend ID 与
runtime `EffectRunner`；reply/send/patch effect payload、interaction-card 稳定幂等键和 patch
card 幂等键均沿用原规则。升级 outbound 的按需构造继续读取 owner 当前 runner，但闭包只捕获
runtime owner 与 frontend ID。移除已无调用方的 `replyInteractionCardEffect`。生产 `*App` 引用
预算由 475 降至 465，app-bearing 函数数由 293 降至 292，持有 App 字段的结构体由 51 降至 42。

步骤 38 删除 `cardRendererForApp(*App)` 这个纯转发 helper，生产调用方直接从已有
`App.Config()` 读取配置并构造 `cardRenderer`。渲染器仍只持有配置，不引入 App 捕获或新的
依赖 owner；renderer 用例覆盖 reply/compact markdown 与按钮内容。生产 `*App` 引用预算由
465 降至 464，app-bearing 函数数由 292 降至 291，持有 App 字段的结构体预算保持 42。
`outboundCardService` 仍是最高扇入候选，后续继续拆其投递、pending-card、message-link 与异步
patch owners。

步骤 39 将卡片标题投影从 `ContentCardTitleForSubmission(*App, ...)` 改为
`ContentCardTitleForSubmissionFromState(SessionStateProvider, ...)`。投影只需要 scoped session
的 workspace 与 collaboration mode；调用时读取当前 session，不冻结 Plan mode 标题。原
Dependencies API 保留并委托到同一 state 投影，维持其它 planmode 调用行为。production
`*App` 引用预算由 464 降至 463，app-bearing 函数数由 291 降至 290，持有 App 字段的结构体
预算保持 42。

步骤 40 将 reply chunk 的拟合、渲染、复用 patch/reply 与文本 fallback 迁入 App-free 的
`replyChunkDelivery` owner。owner 只持有 card renderer、scoped turn state、frontend-scoped
`effectOutbound` 与 Feishu 可用状态；reply-card chunk 大小/组件限制、reasoning-only 消息复用、
patch 幂等键、footer 拆分和部分文本 fallback 语义保持不变。`outboundCardService` 显式持有该
owner，Claude stream、turn items 和 final delivery 共用同一实现。对照 SM-04/26，chunk delivery
仍只负责投递已完成 item 的展示，async user-input pending lifecycle 未改变，也没有触碰
`turn/completed` 收口边界。`replyChunkDelivery` 构造时显式接收 card renderer、turn state、
effect outbound 与 transport readiness，不再新增 `*App` 参数。生产 `*App` 引用预算由 463
降至 456，app-bearing 函数数由 290 降至 283，持有 App 字段的结构体预算保持 42。

步骤 41 将 outbound async user-input 卡从 `sendAsyncUserInputCard(*App, ...)` 拆成
`asyncUserInputCardSender`，显式持有 scoped pending-state provider 与
`pendingCardDeliveryService`。pending delivery owner 只接收 `InteractionDelivery`、frontend
lifecycle/identity、effect deduper/runner 与 turn working-card state；非阻塞请求不写
`waiting_user_input`，卡片仍只复用 reasoning-only working card。对照 SM-26，问题仍独立于
final candidate，答案晚于 turn completion 时仍走同 thread continuation，回调认领和取消
语义未变。生产 `*App` 引用预算由 456 降至 455，app-bearing 函数数由 283 降至 282，持有
App 字段的结构体预算保持 42。

步骤 42 将 local-file preview rewrite 与异步 card patch 迁入 App-free 的
`localFileLinkPatcher`，显式持有配置、scoped session state、Feishu rewriter、frontend
lifecycle/async runner、final-card patch tracker 与 effect outbound。rewrite/patch timeout、
shutdown admission、tracker pending/done 与 final body 更新顺序不变；`outboundCardService`
现在只通过该 owner 安排 final item 的 preview patch。同批将 message-link 写入迁入
App-free 的 `messageLinkRecorder`，每次写入从 runtime owner 解析当前 backend，再通过
Continuation 持久化。生产 `*App` 引用预算由 455 降至 454，
app-bearing 函数数由 282 降至 281，持有 App 字段的结构体预算保持 42。
异步 patch 闭包只捕获显式 owners，惰性 binding-read 预算也由 34 降至 32。

步骤 43 将普通状态消息和带 footer 的最终答复 fallback 迁入 `replyChunkDelivery`，并
复用已有的 chunk renderer、message-link recorder 与 local-file patcher。`outboundCardService`
在回复卡发送失败或无法产出 chunk 时不再回调 `sendTurnEventMessages(*App, ...)` 或
`sendFinalMessagesWithFooter(*App, ...)`；quiet-mode 判断、workspace 本地链接中和、最终卡片
注册与异步 patch 顺序保持不变。回复 chunk 路径的链接仍写入 scoped state，最终答复 fallback
仍通过 Continuation 记录链接。该步只移除结构体方法对 App helper 的传递依赖，预算保持
454 个 `*App` 引用、281 个 app-bearing 函数和 42 个持有 App 字段的结构体；depmap 中
`outboundCardService` 的 App helper 依赖降为 0。

步骤 44 移除 `outboundCardService.app`。服务现在从 `replyChunkDelivery` 读取 scoped
state、动态 frontend 配置、renderer 与 transport readiness；final-footer 查询通过构造时
注入的 `TurnFinalFooterLines` 函数提供。卡片发送、quiet-mode、标题投影、pending input 和
fallback 行为保持不变。该 owner 不再是持有 `*App` 字段的结构体，生产 `*App` 引用预算由
454 降至 453，持有 App 字段的结构体由 42 降至 41，收 `*App` 的函数预算保持 281。

步骤 45 将 `turnStreamOutboundCardAdapter` 改为持有已构造的 `outboundCardService`，并在
`TurnPresentationPorts` 组装时注入。stream 的 plan/item 投递继续走同一组 frontend-scoped
owners，同时去掉 adapter 对 `App` 的字段依赖。生产 `*App` 引用预算由 453 降至 452，
持有 App 字段的结构体预算由 41 降至 40，收 `*App` 的函数预算保持 281。同批在
`ClaudeRuntimePorts` 中把 14 个已就绪的 owner binding 从回调体提升为构造期快照，
保持配置及 runtime 选择的动态读取；lazy binding-read 预算由 32 降至 18。

步骤 46 将 `turnRuntimePort` 从 App 转为 frontend lifecycle、async runner 与
`liveThreadMarker`。marker 显式持有 live-thread tracker、scoped session state、announcement
query 和 refresh coalescer，保留 thread 标记、group chat 识别与公告刷新行为。对照 SM-04，
仅改变 `MarkSessionThreadLive` 的 adapter 依赖，不更改 turn/item 通知顺序及 completed 终态。
生产 `*App` 引用预算由 452 降至 451，持有 App 字段的结构体由 40 降至 39，收 `*App` 的
函数预算保持 281；lazy binding-read 预算保持 18。

步骤 47 将 `turnStreamQuietCardExecutorAdapter` 改为 App-free 的
`quietWorkingCardExecutor`，并与 stream outbound owner 共用 renderer、scoped state、effect
outbound 和 message-link recorder。reply/patch 失败仍不提交 quiet-card render，成功 reply
仍先记录链接再提交 turn-stream 状态。生产 `*App` 引用预算由 451 降至 450，持有 App 字段的
结构体由 39 降至 38，收 `*App` 的函数预算保持 281；lazy binding-read 预算保持 18。

步骤 48 将 `claudeTurnStreamPort` 改为持有 `ItemContext` 与 `TurnPresentation` owners，
移除每次 item callback 都经 `App.Bindings` 转发的结构体字段。port 仍直接调用原 service，
不改变 started/in-flight/completed item 顺序或 stream final 标记。生产 `*App` 引用预算由
450 降至 449，持有 App 字段的结构体由 38 降至 37，收 `*App` 的函数预算保持 281；lazy
binding-read 预算保持 18。

步骤 49 将 `newPlanModeAppAdapter` 闭包中的 ModelSnapshots、Submissions 与 Conversations
改为构造期局部快照。三个 owner 在 adapter 组装时已就绪，计划设置仍通过原 snapshot service
读取，workspace thread 启动与下一 submission 行为不变。lazy binding-read 预算由 18 降至
15；`*App` 引用预算保持 449。

步骤 50 将 `newReviewAppAdapter` 闭包中的 Submissions 和 PendingQueue 改为构造期指针
快照。ReviewCommands 在 composition 中于两个稳定 service pointer 初始化后构造；review
dispatch 仍通过 queue service 处理下一 submission 和 queued reactions。lazy binding-read
预算由 15 降至 13，`*App` 引用预算保持 449。

步骤 51 将 `turnDeliveryPort` 从持有 `*App` 改为持有 scoped state 与现成的
`replyChunkDelivery`。空 final card 的 quiet-mode、mention、复用 patch、卡片失败后的回复文本
fallback、无 trigger 时向 chat 发送文本及 message-link 记录均留在同一行为路径；普通 final
messages 也直接复用 chunk delivery owner。`effectOutbound` 增加 chat 文本发送能力供无 trigger
分支使用。对照 SM-04，本次只替换 delivery adapter 依赖，不改变 turn/item 通知顺序或
`turn/completed` 终态。生产 `*App` 引用预算由 449 降至 448，持有 App 字段的结构体由 37 降至
36，收 `*App` 的函数预算保持 281；lazy binding-read 预算保持 13。

步骤 52 将 `sqAttachmentResolverFullAdapter` 改为显式持有 config、lifecycle context provider
与 Feishu client，并继续委托共享的 `resolveInboundAttachments`。附件目录、源消息选择、30 秒
下载超时和附件字段映射均不变。生产 `*App` 引用预算由 448 降至 447，持有 App 字段的结构体由
36 降至 35，收 `*App` 的函数预算保持 281；lazy binding-read 预算保持 13。

步骤 53 将 `sqBackendRuntimeFullAdapter` 改为持有 `BackendRuntimeDeps` 与 frontend runtime
owner。每次调用从 owner 刷新当前 backend，再构造 facade 和 backend context；Codex→Claude 切换
后不沿用构造时的 facade，当前 Codex/Claude client 仍从 runtime owner 动态读取。
`BackendRuntimeDeps` 对 turn/Claude reconciliation 保留绑定字段的稳定指针，因为 queue 在这两个
service 填充前构造；调用时读取已就绪 owner，不冻结零值。生产 `*App`
引用预算由 447 降至 446，持有 App 字段的结构体由 35 降至 34，收 `*App` 的函数预算保持 281；
lazy binding-read 预算保持 13。

步骤 54 将 `SubmissionPorts` 的 start guard 改为在构造期捕获
`runtimeOwner.SubmissionStarts`，两个回调直接使用该 tracker，并删除只为读取此 tracker 存在的
`submissionStartTracker(*App)`。序列化范围和 TryBegin/Finish 配对不变；生产 `*App` 引用预算由
446 降至 445，收 `*App` 的函数由 281 降至 280，持有 App 字段的结构体预算保持 34；lazy
binding-read 预算保持 13。

步骤 55 将 `modelDefaultsPublisher` 从持有 `*App` 改为显式持有 frontend runtime owner、config
与 config mutex；production composition 和 test fixture 注入同一组窄 owner。只在 Claude defaults
发布时读取当前 Claude core 并更新其配置，Codex 发布仍不触碰 Claude。`*App` 引用预算由 445 降至
443，收 `*App` 的函数由 280 降至 279，持有 App 字段的结构体由 34 降至 33；lazy binding-read
预算保持 13。

步骤 56 将 `maintenanceRepository` 从持有 `*App` 改为显式依赖 scoped state 与 frontend ID。
Sessions 仍按 session key 中的 frontend ID 过滤，PendingRequests 仍从同一 scoped state 读取；
frontend 判断抽成纯 helper 供 config view 与 repository 共用。生产 `*App` 引用预算由 443 降至
441，收 `*App` 的函数由 279 降至 278，持有 App 字段的结构体由 33 降至 32；lazy binding-read
预算保持 13。

步骤 57 将 `ModelSnapshotRepository` 改为显式接收配置、配置锁与 frontend-scoped state，
生产 composition 和测试 fixture 均从 frontend shell 传入这些依赖。模型配置快照仍使用同一
配置修订锁与 frontend scope；生产 `*App` 引用预算由 441 降至 440，收 `*App` 的函数由 278
降至 277，持有 App 字段的结构体预算保持 32；lazy binding-read 预算保持 13。

步骤 58 将 `FrontendFacts` 从闭包捕获 `*App` 改为显式接收 frontend runtime owner 与维护状态，
闭包仍在执行时读取 backend transition 和 message traffic 的当前值，维护状态服务在构造期注入；
生产 `*App` 引用预算由 440 降至 439，收 `*App` 的函数由 277 降至 276，持有 App 字段的结构体
预算保持 32；lazy binding-read 预算保持 13。

步骤 59 将 Claude interaction expiry presenter 改为显式持有 Feishu renderer、scoped state、
frontend identity 与 effect runner；失效卡标题仍按原 session state 添加 workspace/plan 前缀。
生产 `*App` 引用预算由 439 降至 437，收 `*App` 的函数由 276 降至 275，持有 App 字段的
结构体由 32 降至 31；lazy binding-read 预算保持 13。

步骤 60 将 `CodexServiceName` 改为接收配置与共享配置锁，返回的 supplier 仍在调用时读取
最新配置快照，不再捕获 `*App`。生产 `*App` 引用预算由 437 降至 436，收 `*App` 的函数由
275 降至 274，持有 App 字段的结构体预算保持 31；lazy binding-read 预算保持 13。

步骤 61 将 backend configuration builder 改为接收 `BackendConfigurationInputs`，并使用只持有
接口所需配置、锁、backend selector、frontend index、store 与 workspace selection 的本地 adapter
实现 `PermissionDependencies`。composition 和 test fixture 都从已构造对象注入这些值，避免再次
将 App 作为“窄接口”传递；生产 `*App` 引用预算由 436 降至 434，收 `*App` 的函数由 274 降至
272，持有 App 字段的结构体预算保持 31；lazy binding-read 预算保持 13。

步骤 62 将 `upgradeRenderService` 改为显式持有 Feishu status-card renderer 与
`BackendMaintenance` service map。生产 composition 和 test fixture 都在 maintenance service map
构造完成后创建该 presentation owner；升级确认仍调用对应 backend 的原 `Prepare` service，卡片
渲染和 pending request 行为不变。生产 `*App` 引用预算由 434 降至 432，收 `*App` 的函数由
272 降至 271，持有 App 字段的结构体由 31 降至 30；lazy binding-read 预算保持 13。

步骤 63 将 `notificationSender` 改为显式持有 Feishu notification client、frontend ID 与
effect runner。投递使用 Notifications 调用方提供的 lifecycle context 并保留原 5 秒 timeout；
空目标/内容仍跳过，加急失败仍只记录警告。生产 `*App` 引用预算由 432 降至 429，收 `*App` 的
函数由 271 降至 269，持有 App 字段的结构体由 30 降至 29；lazy binding-read 预算保持 13。

步骤 64 将 `feishuEventRouter` 改为显式持有启动时间、inbound service、消息 deduper、traffic
counter 与失败回复能力。dispatcher 组合失败回复时只捕获 frontend runtime owner、frontend ID
和配置值；消息过期检查、去重 claim/release/mark-done、流量计数、recall/reaction 丢弃行为与
错误 effect 均保持原有顺序和生命周期。生产 `*App` 引用由 429 降至 427，收 `*App` 的函数由
269 降至 268，持有 App 字段的结构体由 29 降至 28；lazy binding-read 预算保持 13。

步骤 65 将 `inboundRouting` 改为持有 frontend ID、Feishu client、group-primary services 与
`GroupMessages`，移除其 `*App` 字段。group primary assignment 同步改为接收这些显式依赖；
`LiveBotOpenID` supplier 只捕获 Feishu client，group delivery gate 直接调用 `GroupMessages`。
primary 初始化、stale assignment 判断、非目标 bot 关闭 primary、`@所有人` 旁路及消息提及判断
沿用原有顺序与决策。生产 `*App` 引用由 427 降至 420，收 `*App` 的函数由 268 降至 262，
持有 App 字段的结构体由 28 降至 27；lazy binding-read 预算保持 13。

步骤 66 将 group announcement status builders 和 Bot display-name helper 改为显式接收
Feishu client、frontend/backend 快照及 announcement/conversation queries；删除忽略 chatID 的
`groupAnnouncementBotOpenID(*App, ...)` 包装器。状态内容、common region、空名称 fallback 和
刷新时读取的动态 backend/configuration 均保持原行为。生产 `*App` 引用由 420 降至 416，收
`*App` 的函数由 262 降至 258，持有 App 字段的结构体预算保持 27；lazy binding-read 预算保持 13。

步骤 67 将 group announcement 的 coalesced refresh 闭包改为捕获具名依赖包，不再持有
`*App`；配置 workspace repository 改由 config 指针、锁和路径构造，避免 query 间接保留
frontend aggregate。各调度入口只接收 coalescer/query，backend 仍在每次刷新时从 runtime owner
读取并在 config 锁下回退到当前 frontend 配置。coalescing、frontend lifecycle 和刷新 timeout
不变，并增加 runtime backend 动态读取测试。生产 `*App` 引用由 416 降至 411，收 `*App` 的函数由
258 降至 253，持有 App 字段的结构体预算保持 27；lazy binding-read 预算保持 13。

步骤 68 将 `modelWriteAdmission` 改为持有 `frontend.Query`，并让 blocked-reason helper 直接接收
该 query；composition 与 fixture 都从已经构造的 `Bindings.FrontendQuery` 注入 admission。模型配置
写入仍只检查 frontend 的维护/切换事实，零值 query 仍允许写入。生产 `*App` 引用由 411 降至 408，
收 `*App` 的函数由 253 降至 251，持有 App 字段的结构体由 27 降至 26；lazy binding-read 预算保持 13。

步骤 69 将 `ensureSessionModelConfigWritable` 也改为接收 `frontend.Query`，其四个调用点从
binding command owner 传入已构造的 frontend query，不再把 App 传给 guard。模型设置和命令路径继续
使用同一 frontend activity policy。生产 `*App` 引用由 408 降至 407，收 `*App` 的函数由 251 降至
250，持有 App 字段的结构体预算保持 26；lazy binding-read 预算保持 13。

步骤 70 将 `modelConfigStatus` 改为接收 snapshot service、scoped state 和 frontend config view，
配置副本读取统一直接使用既有 config+lock helper。`BuildModelCommands` 的状态 callback 在构造期捕获
这些明确依赖，执行时仍通过 runtime owner 读取当前 backend。生产 `*App` 引用由 407 降至 405，收
`*App` 的函数由 250 降至 248，持有 App 字段的结构体预算保持 26；lazy binding-read 预算保持 13。

步骤 71 将 `sessionScopedConfigForApp` 改为值型 `sessionModelConfigSource`，只持有配置/锁、scoped
store、snapshot service、frontend config identity 和动态 backend supplier。p2p session scope 解析保留
原始/规范化 key 与 session metadata 的 fallback；group metadata 不会被误判为 p2p。生产 `*App` 引用由
405 降至 404，收 `*App` 的函数由 248 降至 247，持有 App 字段的结构体预算保持 26；lazy binding-read
预算保持 13。

步骤 72 将 `ConversationControlPorts` 改为接收具名 inputs，`conversationRuntimeControl` 改为持有
`BackendRuntimeDeps` 与 conversation service；runtime helper 每次按 owner 当前 backend 选择 facade，
interrupt 仍只在 Codex error 后做完成状态对账。fixture 与 production composition 同步使用同一依赖形状。
生产 `*App` 引用由 404 降至 401，收 `*App` 的函数由 247 降至 245，App-bearing `*Ports` 工厂由 16
降至 15，持有 App 字段的结构体由 26 降至 25；lazy binding-read 预算保持 13。

步骤 73 将 `ConversationPorts` 改为接收 `ConversationPortInputs`，显式传入 frontend-scoped
repository、model settings、thread binding、conversation configuration 与 runtime owner。backend、Claude
core 和 Codex client 仍从 runtime owner 动态读取；新增测试覆盖 backend override 变化及清空后的配置回退。
生产 `*App` 引用由 401 降至 400，收 `*App` 的函数由 245 降至 244，App-bearing `*Ports` 工厂由 15
降至 14，持有 App 字段的结构体预算保持 25；lazy binding-read 预算保持 13。

步骤 74 将 Submission workspace 解析和 inflight mode helper 改为接收 scoped state、workspace
selection、default workspace supplier 与 backend supplier；`SubmissionPorts` 的配置闭包改为捕获配置视图，
backend 仍通过 runtime owner 动态查询。生产 `*App` 引用由 400 降至 398，收 `*App` 的函数由 244 降至
242，App-bearing `*Ports` 工厂保持 14，持有 App 字段的结构体保持 25；lazy binding-read 预算保持 13。

步骤 75 将 queued notice 从 `sendSubmissionQueuedNotice(*App, ...)` 移到 `outboundCardService`，
submission 与 review delivery 均委托给该 owner。quiet-mode gate、compact card fallback 与 scoped message-link
持久化沿用 `replyChunkDelivery`；生产 `*App` 引用由 398 降至 397，收 `*App` 的函数由 242 降至 241，
App-bearing `*Ports` 工厂保持 14，持有 App 字段的结构体保持 25；lazy binding-read 预算保持 13。

步骤 76 删除 `replyTextByAnchorEffect(*App, ...)` 并改由 `effectOutbound` 执行；`SubmissionPorts` 的
reply/session-async callbacks 改捕获 effect outbound、frontend lifecycle、session actors 与 scoped state，异步
工作仍先通过 lifecycle admission 再按 session 串行。生产 `*App` 引用由 397 降至 396，收 `*App` 的函数由
241 降至 240，App-bearing `*Ports` 工厂保持 14，持有 App 字段的结构体保持 25；lazy binding-read 预算保持 13。

步骤 77 将 plan confirmation expiry 的卡片渲染与 patch 从 `SubmissionPorts` 的 App 闭包移至
`outboundCardService`。过期提示仍使用当前 scoped session 的 workspace/plan 标题、原灰色卡片与提示文案；新增
回归测试验证渲染和 patch。该 helper 原本只经已有的 `newPlanModeAppAdapter(a)` 间接使用 App，没有减少 AST
`*App` 数量，本步只缩小 callback 的运行时捕获边界；接下来继续把 `SubmissionPorts` 改为显式 inputs。

步骤 78 将 `SubmissionPorts(*App, ...)` 改为 `SubmissionPorts(SubmissionPortInputs)`，production composition
与 test fixture 注入 state/config、runtime owner/deps、session actor、queue/turn/review services 和 notice/reply
ports。queued 与 expiry notice 的构造另改为接收 config view、scoped state、runtime owner、continuation、Feishu
client 与 effect runner；backend/client 仍在执行时从 owner 查询，session async 仍经过 lifecycle admission 后由
session actor 串行。生产 `*App` 引用由 395 的前一基线 396 降至 395，收 `*App` 的函数由 240 降至 239，
App-bearing `*Ports` 工厂由 14 降至 13，持有 App 字段的结构体保持 25；lazy binding-read 预算保持 13。

步骤 79 将 `inboundBackend` 从持有 `*App` 改为持有当前 backend supplier、backend-selection callback、切换状态
supplier 与 `BackendRuntimeDeps`。每次 inbound admission 仍读取 runtime owner 当前 backend，并用当前 runtime
context 检查维护状态；Claude maintenance 期间非本地消息仍被拒绝，backend selection 提示与切换拦截行为不变。
`TestClaudeUpgradeBlocksCommandsAndInboundMessages` 覆盖该 admission 边界。生产 `*App` 引用由 395 降至 394，
App-bearing 结构体由 25 降至 24；收 `*App` 的函数保持 239，App-bearing `*Ports` 工厂保持 13，lazy
binding-read 预算保持 13。此改动不改变 pending request、thread binding 或 turn lifecycle 状态转换。

步骤 80 将 `AutoRetryPorts(*App, ...)` 改为 `AutoRetryPorts(AutoRetryPortInputs)`，显式注入 retry tracker、scoped
repository、runtime owner、queue starter、config view、presenter 与 async runner。engine 仍在 submission queue
构造前创建并由 queue 按值捕获；composition 持有的 `BackendRuntimeDeps` 指针在 dispatcher 和 runtime owners
就绪后填充，定时 dispatch 仍从当前 runtime owner 读取 backend、经 lifecycle admission 后 dispatch 原
`RetryTimerFired` input。生产 `*App` 引用由 394 降至 393，收 `*App` 的函数由 239 降至 238，App-bearing
`*Ports` 工厂由 13 降至 12，App-bearing 结构体保持 24；lazy binding-read 预算由 13 降至 12。自动重试优先级、
失败后重排、停止后失效与 turn terminal presentation 用例覆盖行为边界。

步骤 81 将 Claude runtime lifecycle callbacks 改为显式使用 session actors、conversation service/query、scoped
state 与 `BackendRuntimeDeps`。Claude thread binding helper 不再接收 `*App`；thread/turn lifecycle callbacks
继续使用同一 `session:<key>` actor 串行执行，turn 完成按 backend thread 查询 session 后再调用原 `FinishTurn`，
steer completion 与 backend failure 的状态 owner 和执行顺序不变。对照 SM-03/SM-04：不改变 thread ID 绑定、
turn terminal 识别或 session 状态收口。生产 `*App` 引用由 393 降至 392，收 `*App` 的函数由 238 降至 237；
App-bearing 结构体与 `*Ports` 工厂数保持 24、12，lazy binding-read 预算保持 12。`claude_core_test.go` 中
现有 ready binding 用例覆盖 thread binding 保留 root turn binding 的行为。

步骤 82 移除 `executeQuietWorkingCardOp(*App, ...)` helper。Claude runtime ports 直接使用由现有 outbound card
owner 构造的 `quietWorkingCardExecutor`；执行仍先成功 patch/reply，再 commit turn-stream quiet render state，失败
时不推进该状态。原 executor 测试改为显式构造 renderer/state/outbound/link/turn owners。生产 `*App` 引用由 392
降至 391，收 `*App` 的函数由 237 降至 236；`ClaudeRuntimePorts` direct/helper/structs 维持 5/6/0，App-bearing
工厂数与结构体数保持 12、24，lazy binding-read 预算保持 12。该步骤只调整 Feishu delivery adapter 装配，不改变
SM-04 的 turn/item 事件、最终消息复用或 terminal 语义。

步骤 83 将 Claude output-segment update/finalize 从三个 `*App` helper 收进显式
`claudeOutputSegmentDelivery` owner，并复用 `newOutboundCardService` 已构造的 reply chunk delivery、submission
lookup、turn-final footer 与 stream-final marker。quiet-mode gate、chunk/reuse、message-link 记录，以及成功投递后
才标记 stream final 的顺序保持不变；final 继续直接使用 `SendWithReuseIDs`，不触发 final-card patch 注册等额外
副作用。对照 SM-04：仅改变 Feishu delivery adapter 的依赖传递，不改变 turn/item/terminal 状态机。生产 `*App`
引用由 391 降至 388，收 `*App` 的函数由 236 降至 233；`ClaudeRuntimePorts` direct/helper/structs 由 5/6/0
降至 5/4/0，App-bearing 工厂与结构体数保持 12、24，lazy binding-read 预算保持 12。

步骤 84 将 Claude 后台任务通知从 `sendClaudeBackgroundTaskNotification(*App, ...)` 改为显式
`claudeBackgroundTaskNotifier`，由 Claude runtime factory 注入 scoped state、Feishu status-card client、frontend
identity 与 effect runner。标题仍依据当前 session 的 workspace/plan 状态生成；通知仍携带原 chat 与 parent
message 身份，reply 失败后仍尝试向原 chat 独立发送。通知的 status、内容与 fallback 测试通过。生产 `*App`
引用由 388 降至 386，收 `*App` 的函数由 233 降至 231；Staticcheck 随后发现只被该 helper 使用的
`sendCardEffect(*App, ...)` 已无调用点，因此一并删除。`ClaudeRuntimePorts` direct/helper/structs 由 5/4/0
降至 5/3/0；App-bearing 工厂与结构体数保持 12、24，lazy binding-read 预算保持 12。

步骤 85 将 `sendFinalMessagesWithFooterAndReuse(*App, ...)` 改为消费已构造的
`replyChunkDelivery`，Claude runtime 与 turn lifecycle 继续复用各自已装配的 outbound owner，App 包装入口也只
在边界处构造 owner。quiet-mode gate、trigger 校验、footer chunk、preview、reuse IDs 和成功结果保持不变；仍不
触发 final-card patch 注册或链接记录副作用。对照 SM-04：仅调整 final delivery adapter 的依赖传递，不改变
turn terminal 确认或 stream-final 标记顺序。生产 `*App` 引用由 386 降至 385，收 `*App` 的函数由 231 降至
230；`ClaudeRuntimePorts` direct/helper/structs 由 5/3/0 降至 5/2/0，App-bearing 工厂与结构体数保持 12、24，
lazy binding-read 预算保持 12。

步骤 86 将 `prepareClaudeMCPConfig(*App, ...)` 改为显式接收 config、frontend runtime owner 与 MCP service，并
删除只供其使用的 `currentMCPPublication(*App)`。runtime owner 的 MCP started 状态仍在 callback 执行时查询，
配置 nil 时仍返回空结果；Claude factory 闭包捕获的是构造期快照。生产 `*App` 引用由 385 降至 383，收 `*App`
的函数由 230 降至 228；`ClaudeRuntimePorts` direct/helper/structs 由 5/2/0 降至 5/1/0，App-bearing 工厂与
结构体数保持 12、24，lazy binding-read 预算保持 12。

步骤 87 将 inbound pending group gate 从 App-bearing `bindingService` method value 中移出，使用独立
`pendingGroupMessageGate` 显式持有 pending/primary services、config view、runtime context、workspace renderer 与
effect runner；`inboundBindings` 改为 App-free，并删除 `discardPendingBindingMessageByID(*App, ...)`。保持
group/chat-id gate、primary 查询失败时按非 primary 处理、pending effects 执行后再回复 workspace menu，以及
discard 失败日志和空 state 快速返回。生产 `*App` 引用由 383 降至 381，收 `*App` 的函数由 228 降至 227，
App-bearing 结构体由 24 降至 23；InboundPorts 的 `a.X`/bindings/helpers/structs 实际依赖为 12/10/1/3，
惰性读取预算保持 12。该步只调整 inbound adapter 的依赖传递，不改变 Codex thread/turn 状态机。

步骤 88 将 `inboundRootInputs` 从持有 `*App` 改为窄回调，并将 plan-mode text completion 改为直接接收
Claude support service；因此移除只服务该 inbound 分支的 App-bearing `pendingInputService` 和
`handleRootPendingTextResponse(*App, ...)`。workspace-new 与 Claude plan feedback 的 kind dispatch、空输入校验及
后续 application service 调用保持不变。生产 `*App` 引用由 381 降至 377，收 `*App` 的函数由 227 降至 226，
App-bearing 结构体由 23 降至 21；InboundPorts 的 `a.X`/bindings/helpers/structs 为 12/13/1/1，lazy binding-read
预算仍为 12。

步骤 89 删除重复的 `sendEmptyFinalCardWithReuse(*App, ...)` 与 `sendEmptyFinalCard(*App, ...)`，改由现有
`replyChunkDelivery.SendEmptyFinalCardWithReuse` 统一处理空 final card；Feishu App method 和测试调用点直接转到
该 owner。quiet-mode、attention mention、footer、reuse patch、message-link 及卡片/文本/chat fallback 均由原
delivery owner 覆盖，删除的实现与 owner 行为一致。生产 `*App` 引用由 377 降至 375，收 `*App` 的函数由 226
降至 224，App-bearing 结构体保持 21；工厂依赖表未增加，lazy binding-read 预算仍为 12。

步骤 90 删除已无生产调用点的 `sendFinalMessages(*App, ...)` 与
`sendFinalMessagesWithFooter(*App, ...)` wrapper；final delivery 测试直接调用现有
`replyChunkDelivery.SendFinalMessagesWithFooter`，覆盖拆卡、footer、attention mention 与不可用 owner 的空结果。
线上路径此前已由 turn/Claude delivery owner 直接调用 `replyChunkDelivery`，行为不变。生产 `*App` 引用由
375 降至 373，收 `*App` 的函数由 224 降至 222，App-bearing 结构体保持 21；lazy binding-read 预算仍为 12。

步骤 91 删除只转调 `a.runtimeView().requireCodexGateway()` 的 `requireCodexGateway(*App)`，将动态 Codex
gateway 查询改为调用点直接使用 runtime view。client 替换、移除和未初始化时的错误行为由既有 runtime-view 与
Plan/Review/ModelConfig/Turn 测试覆盖；对照 SM-03/04，仅改变 gateway accessor 的依赖传递，thread/turn 请求
顺序不变。生产 `*App` 引用由 373 降至 372，收 `*App` 的函数由 222 降至 221，App-bearing 结构体保持 21；
lazy binding-read 预算仍为 12。

步骤 92 将 `recoveryState` 与 `replyCodexError` 从接收 `*App` 改为接收 `runtimeView`，由 recovery 和
server-request composition 显式传入 runtime client owner。recovery state 仍 frontend-scoped，错误回复仍发给
当前 Codex client；对照 SM-09/10/22/23，不改变 pending request 的 reply/resolved 边界。生产 `*App` 引用由
372 降至 370，收 `*App` 的函数由 221 降至 219，App-bearing 结构体保持 21；lazy binding-read 预算仍为 12。

步骤 93 在注册 Feishu group-message policy callback 前捕获已构造的 `GroupMessages` service，移除闭包内的
`a.bindings.GroupMessages` 惰性读取。callback 仍使用同一 policy owner 和相同的 root/reply/mention 输入映射；
架构计数只将 lazy binding-read 预算由 12 降至 11，生产 `*App` 引用与函数预算不变。

步骤 94 `buildBackendSelectionService` 在两个 composition 入口中都晚于 `StartupRecovery` 和 `AutoRetry` 的
赋值，因此构造时捕获这两个 ready service，移除 RecoverState 与 CommandAutoRetry callback 内的 binding 读取。
backend recovery、auto-retry 命令仍委托原 owners；lazy binding-read 预算由 11 降至 9，`*App` 引用预算不变。

步骤 95 `newInputDispatcher` 在 auto-retry owner 装配完成后构造，Retry handler 改为捕获已就绪的 `AutoRetry`
service，移除 callback 内的 binding 读取。enqueue effect runner 在 composition 初始化早期创建，先于 submission
queue，仍需在 effect 执行时读取该 queue；保留这处真实的晚绑定。lazy binding-read 预算由 9 降至 8，`*App`
引用预算不变。

步骤 96 `mcpDependenciesForApp` 在 `BuildMCP` 时捕获已初始化的 `TurnItems` tracker；MCP callback 每次仍从该
tracker 查询最新 started-item 状态，只移除对 App bindings 字段的惰性查找。lazy binding-read 预算由 8 降至 7，
`*App` 引用预算不变。

步骤 97 将 `BuildUpgrades` 移到 `WorkspaceConfiguration` 构造之后，并显式接收该 config service；同时在 factory
调用时捕获已就绪的 workspace presentation。CurrentWorkspace 与 path-picker callback 不再从 App bindings 延迟读取；
upgrade/workspace 选择仍委托原 services，production 与 test fixture 构造顺序一致。lazy binding-read 预算由 7 降至 5，
`*App` 引用预算不变。

步骤 98 Goal card action 收到请求时已在 composition 完成之后；`completeMenuGoalAsync` 与
`completeGoalRenderedActionAsync` 在提交 async work 前复制已就绪的 `GoalCommands` service，closure 捕获该 service
值而不是延迟读取 `App.bindings`。卡片响应、toast 和异步 patch 流程不变；lazy binding-read 预算由 5 降至 3，
`*App` 引用预算不变。

步骤 99 将 `StartupRecovery` 构造移到 `ConversationRecovery` 之后，并把已就绪的
`ConversationRecovery.Restore` 作为显式依赖传给 `StartupRecoveryPorts`；`BackendSelection` 与 inbound ports 随后
组装，所需 owners 均已就绪。Codex transport recovery 仍通过 composition 根部连接 startup recovery callback，
StartupRecovery 执行时的 reset/begin/restore 顺序不变。lazy binding-read 预算由 3 降至 2，`*App` 引用预算不变。

步骤 100 让 Codex recovery 与 effect runner 在构造时捕获预先分配的 submission queue 指针。composition 和测试 fixture 都先挂入该占位 owner，再原地填充 queue service；effect runner 因而移到 bindings 挂接之后创建。异步 recovery、`EnqueueInput` 校验和队列调用行为不变；对照 SM-03，恢复与 turn/session 状态迁移顺序不变。lazy binding-read 预算由 2 降至 0，`*App` 引用预算不变。

步骤 101 将 `startFrontend` 的依赖从 `*App` 缩为 Feishu client 与 context；Serve 仍以同一 context 启动同一 transport，生产 `*App` 引用预算由 370 降至 369，lazy binding-read 预算保持 0。

步骤 102 删除只被 `Prepare` 调用的 `startBackend` 包装及只转调 `App.SetBackend` 的 `setRuntimeBackend`；调用点继续构造当前 runtime handle 并用同一 backend 值更新 frontend owner，生产 `*App` 引用预算由 369 降至 367，lazy binding-read 预算保持 0。

步骤 103 将 `currentBackendRuntimeHandle` 改为接收 backend 值与 `runtimeView`，client snapshot 仍来自同一 frontend runtime owner；生产 `*App` 引用预算由 367 降至 366，lazy binding-read 预算保持 0。

步骤 104 将 `backendRuntime` 改为接收 backend kind；commands 复用已读取的 kind，recovery、accessor 与 thread-menu 在调用点从 config view 读取当前值。空 backend 仍映射为 nil facade，生产 `*App` 引用预算由 366 降至 365，lazy binding-read 预算保持 0。

步骤 105 将 `BuildFinalCardPatch` 改为接收显式 context、tracker、finder、patcher、renderer state/config 与 async runner；final card 的标题解析、渲染、footer 和 patch 调用不变。生产 `*App` 引用预算由 365 降至 364，lazy binding-read 预算保持 0。

步骤 106 删除 startup recovery 的两个单点 App 包装；Codex recovery 直接复用 live-thread tracker owner，后台通知闭包捕获已就绪的 StartupRecovery 值。恢复状态清理与通知调度时序不变，生产 `*App` 引用预算由 364 降至 362，lazy binding-read 预算保持 0。

步骤 107 将 `threadMenuBackendRuntimeAdapter` 的 `*App` 字段替换为 `BackendRuntimeDeps` 快照；两个 runtime operation 仍使用当前 config/runtime view 和同一 facade，adapter 不再持有聚合。生产 `*App` 引用预算由 362 降至 361，lazy binding-read 预算保持 0。

步骤 108 将 `threadMenuConversationBackendAdapter` 改为持有 scoped store、config/backend supplier、conversation service 与 `BackendRuntimeDeps`；线程列表和 fork 提示 helper 也改为显式输入。线程查询、fork 提示、interrupt 和 resume 仍委托原 owners，生产 `*App` 引用预算由 361 降至 358，lazy binding-read 预算保持 0。

步骤 109 将 `threadMenuWorkspaceConfigAdapter` 改为持有 workspace configuration service 和 backend supplier；当前 thread 选择与缺少活动线程时的 backend-specific 提示不变。生产 `*App` 引用预算由 358 降至 356，lazy binding-read 预算保持 0。

步骤 110 将 Goal continuation dependencies 直接在 composition root 中装配，不再由 `GoalContinuationPorts(*App)` 读取聚合字段；配置查询和 workspace selection 默认值改为接收 config/mutex，frontend 隔离仍按相同 session key parser 判断。对照 SM-25，后台 continuation 仍只创建合成 submission 和新的 Feishu anchor，不触发本地 `turn/start`；生产 `*App` 引用预算由 356 降至 354，lazy binding-read 预算保持 0。

步骤 111 将 debug usage、runtime state 与 workspace adapters 从持有整个 `*App` 改为持有 turn-binding tracker、tracker-backed usage renderer/backend supplier、workspace config service 和 picker renderer，并删除因此无用的聚合 helper。该改动不改变 debug/usage 查询或卡片渲染行为；生产 `*App` 引用预算由 354 降至 349，App-bearing 结构体由 21 降至 17，lazy binding-read 预算保持 0。

步骤 112 将 Debug、Usage 与 FileSharePorts 的 service 工厂改为接收已构造的 debug dependencies 和 runtime owners；App 适配只留在依赖装配入口。download session actor 继续在 frontend lifecycle admission 后执行；生产 `*App` 引用预算由 349 降至 346，收 `*App` 的顶层函数由 221 降至 219，收 `*App` 的 `*Ports` 工厂由 11 降至 10，lazy binding-read 预算保持 0。

步骤 113 将 group binding session 查找与 workspace 解析改为使用 `bindingSessionScope` 的 scoped state、normalizer、primary lookup 和 frontend ID，不再把 `*App` 传入 binding lookup helper。group chat type 的 fallback 仍先查 agent binding，再查 primary 状态；生产 `*App` 引用预算由 346 降至 344，收 `*App` 的顶层函数由 219 降至 217，lazy binding-read 预算保持 0。

步骤 114 将 primary 初始化、查询、状态判断与写入 helpers 改为接收既有 routing services、frontend ID 和 Feishu client，不再从 `*App` 中取 primary owner。命令状态卡、primary assignment 和 bot-added 初始化仍走同一 application owners；生产 `*App` 引用预算由 344 降至 338，收 `*App` 的顶层函数由 217 降至 211，lazy binding-read 预算保持 0。

步骤 115 删除 App-bearing live-thread tracker/session marker helpers，并移除无调用方的 `App.MarkSessionThreadLive` 方法。Turn runtime 已有的 `liveThreadMarker` 复用于 workspace thread 创建；workspace 清理及 Codex/startup recovery 在构造时直接捕获 frontend-owned tracker。session mark 仍按原顺序更新 tracker，再读取 session 并刷新 group announcement；clear/reset 仍只影响当前 frontend tracker。对照状态机审计 SM-03：这只操作当前 frontend 的易失 attached-thread 标记，不改变 thread ID/session binding、thread start/resume 或 lifecycle 转换。测试兼容 helper 留在 `_test.go`。生产 `*App` 引用预算由 338 降至 332，收 `*App` 的顶层函数由 211 降至 205，lazy binding-read 预算保持 0。

步骤 116 将 compact 标题与 preparing/accepted/failed card renderer 改为接收 scoped session state，并直接调用 planmode 的 state-based 标题投影，不再要求 `*App`。workspace 与 `[plan]` 标题前缀继续从相同 session 状态生成。生产 `*App` 引用预算由 332 降至 328，收 `*App` 的顶层函数由 205 降至 201，lazy binding-read 预算保持 0。

步骤 117 将 frontend idle 与 message-traffic allowance helpers 改为接收 `frontend.Query`；backend-selection runtime 在装配时捕获同一个 query，不再把 `*App` 传给 idle gate。session、pending request、retry、maintenance 和 message-traffic 的判定继续由同一 Query/Activity owner 执行，nil repository 仍返回未初始化提示。生产 `*App` 引用预算由 328 降至 324，收 `*App` 的顶层函数由 201 降至 197，lazy binding-read 预算保持 0。

步骤 118 将状态卡 renderer 改为显式接收 scoped state、Feishu renderer、backend kind 与已生成的状态正文。title 投影继续使用 state-based workspace/plan helper，保持 `[workspace] [plan]` 前缀；Status 正文仍由 backend configuration owner 生成。生产 `*App` 引用预算由 324 降至 323，收 `*App` 的顶层函数由 197 降至 196，lazy binding-read 预算保持 0。

步骤 119 将共享 approval card renderer 改为显式接收 scoped session state 与 Feishu card renderer；Claude support 和 server-request adapter 在组合边界传入已持有的值。workspace/plan 标题投影、attention mention、颜色、正文和按钮保持不变。生产 `*App` 引用预算由 323 降至 322，收 `*App` 的顶层函数由 196 降至 194，lazy binding-read 预算保持 0。

步骤 120 将 interrupt preparing/result/failed card renderer 改为显式接收 scoped session state 与 Feishu card renderer；backend action composition 捕获构造期 state/client。workspace/plan 标题、parent action 返回按钮、重试按钮和提示正文不变。生产 `*App` 引用预算由 322 降至 319，收 `*App` 的顶层函数由 194 降至 191，lazy binding-read 预算保持 0。

步骤 121 将 pending backend fallback 改为接收 `frontendConfigView`，Codex goal gateway accessor 改为接收当前 Codex client，并把 Goal/Plan 重复的异步回复线程判断合并为 state lookup + config boolean helper。pending request 中明确记录的 backend 仍优先于当前 frontend 配置；未初始化 Codex client 的错误文本不变。对照 SM-25，gateway 获取依赖改变，不改变 goal 请求、通知或 continuation turn 处理。生产 `*App` 引用预算由 319 降至 315，收 `*App` 的顶层函数由 191 降至 187，lazy binding-read 预算保持 0。

步骤 122 将 permission runtime、session task 与 failure-card ports 拆成独立窄依赖；Claude core/backend 仍在执行时动态查询，后台任务仍经 frontend lifecycle admission 和同一 session actor，失败卡片仍以当前 config/session 重绘并通过原 effect runner patch。backend permission renderer 只要求配置读取能力。对照状态机审计中权限设置的保存、运行时应用与失败提示顺序，本次仅调整依赖装配，不改变生命周期或权限迁移。生产 `*App` 引用预算由 315 降至 310，收 `*App` 的顶层函数由 187 降至 183，App-bearing 结构体由 14 降至 13，lazy binding-read 预算保持 0。

步骤 123 将 Codex server-request reply adapter 的 `*App` 字段改为 frontend runtime owner 与 frontend ID；reply context 和 effect runner 仍在调用时从同一 owner 获取，response payload 与 backend target 不变。生产 `*App` 引用预算由 310 降至 309，App-bearing 结构体由 13 降至 12，lazy binding-read 预算保持 0。

步骤 124 将 review workspace、Git options、target resolver 与 queued-review dispatcher 的 `*App` 字段分别替换为 config/session、lifecycle context、submission queue 和 pending queue；ReviewPorts 与 review form composition 的调用顺序和服务调用保持不变。生产 `*App` 引用预算由 309 降至 304，App-bearing 结构体由 12 降至 7，lazy binding-read 预算保持 0。

步骤 125 将 `turnReconciliationGateway` 改为接收 `BackendRuntimeDeps`；backend 选择与 Codex client availability 仍在调用时从当前 frontend runtime 查询，thread turn 读取仍使用同一 Codex gateway。生产 `*App` 引用预算由 304 降至 302，收 `*App` 的顶层函数由 183 降至 182，App-bearing 结构体由 7 降至 6，lazy binding-read 预算保持 0。

步骤 126 将 Claude stopped-session 查询改为接收 backend 与 Claude core suppliers；core 与 backend 仍在每次查询时动态取得，停止判定逻辑不变。生产 `*App` 引用预算由 302 降至 301，收 `*App` 的顶层函数由 182 降至 181，lazy binding-read 预算保持 0。

步骤 127 将 `ForwardFailure` 改为接收 lifecycle context、frontend ID 与 effect runner，并抽出显式依赖的错误回复 helper；forward 处理失败仍使用原 frontend 的生命周期 context 与 Feishu effect runner。生产 `*App` 引用预算由 301 降至 300，收 `*App` 的顶层函数由 181 降至 180，lazy binding-read 预算保持 0。

步骤 128 将 `ForwardProcessor` 改为接收 frontend session actors 与 session-key builder；处理仍在同一 session actor 内串行执行，key 继续按当前 frontend 规范化。生产 `*App` 引用预算由 300 降至 299，收 `*App` 的顶层函数由 180 降至 179，lazy binding-read 预算保持 0。

步骤 129 删除生产代码中已无调用方的 `replyError(*App, ...)` 包装器；回复逻辑统一由显式 context/frontend/effect-runner helper 执行，测试通过 test-only fixture 调用同一 helper。生产 `*App` 引用预算由 299 降至 298，收 `*App` 的顶层函数由 179 降至 178，lazy binding-read 预算保持 0。

步骤 130 将 session-key migration helper 改为接收持久化 `state.Store`，不再经 `*App` 读取 store；key 规范化与 auxiliary-key 保留规则未变。生产 `*App` 引用预算由 298 降至 296，收 `*App` 的顶层函数由 178 降至 176，lazy binding-read 预算保持 0。

步骤 131 将 backend runtime handle builder 改为接收 `BackendRuntimeDeps`；composition 在构造 handle 时传入能力快照，backend runtime context 构建保持原有配置与 runtime owner。生产 `*App` 引用预算由 296 降至 294，收 `*App` 的顶层函数由 176 降至 174，lazy binding-read 预算保持 0。

步骤 132 将 backend runtime install 改为接收 `BackendRuntimeDeps`，并把 scoped state view 纳入该能力包；runtime backend、Codex recovery client、Claude core 与 scoped state 的更新仍按原顺序执行。生产 `*App` 引用预算由 294 降至 292，收 `*App` 的顶层函数由 174 降至 172，lazy binding-read 预算保持 0。

步骤 133 将 application backend-switch ports 及 available/prepare/snapshot/ready runtime helpers 改为接收 `BackendRuntimeDeps`、frontend query、transition、startup recovery 与 announcement owners；prepare/snapshot 的 Install 闭包也仅捕获 runtime deps。配置 repository 使用显式的 frontend-scoped source adapter，切换状态迁移、idle gate 与 recovery 顺序不变。生产 `*App` 引用预算由 292 降至 284，收 `*App` 的顶层函数由 172 降至 164，App-bearing 结构体由 6 降至 5，App-bearing `*Ports` 工厂由 10 降至 9，lazy binding-read 预算保持 0。

步骤 134 将 `InboundPorts` 改为接收 frontend identity/context/session-key、routing/request/submission services、backend/runtime suppliers、effect runner 与明确的回调；`inboundCommands` 只持有 backend supplier 和命令 handler，不再持有 `*App`。group pending gate 继续使用同一 session key、lifecycle context 和 effect runner，workspace/plan 文本回复、附件解析、backend admission、通知刷新与 merge-forward 预取路径不变。原 `handleCommand` 入口改为导出 `HandleInboundCommand`，测试调用同步迁移；命令语义未变，并删除已无调用方的通知刷新 helper。本步对照 SM-09/10/11/22/23/26：不改变 server request reply/resolved、pending input 或 submission 排队状态边界。生产 `*App` 引用预算由 284 降至 281，AST 中直接收 `*App` 的函数为 147，App-bearing 结构体由 5 降至 4，App-bearing `*Ports` 工厂由 9 降至 8，lazy binding-read 预算保持 0。

步骤 135 将 `ClaudeRuntimePorts(*App, cfg)` 改为 `ClaudeRuntimePorts(ClaudeRuntimePortInputs)`，production composition 在 Claude runtime 创建时显式提供 backend runtime deps、card delivery、submission/model/interaction/turn/conversation owners；test-only fixture 在 `_test.go` 中保留便捷装配。factory 创建时仍动态取得当前 backend/runtime deps，Claude lifecycle、model acknowledgement、MCP 配置与卡片投递回调行为不变。本步对照 SM-03/04/08/15：只调整 runtime adapter 的依赖装配，不改变 session actor 串行边界、turn 完成语义、模型设置确认时机或 MCP 生命周期。生产 `*App` 引用预算由 281 降至 280，AST 中直接收 `*App` 的函数由 147 降至 146，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 8 降至 7，lazy binding-read 预算保持 0；`ClaudeRuntimePorts` 自身及其传递依赖不再含 `*App`。

步骤 136 将 `TurnPorts(*App, ...)` 改为 `TurnPorts(TurnPortInputs)`，turn lifecycle、queue、retry、cleanup、runtime、card delivery 与 continuation owners 在 composition 显式传入；plan-mode completion dependencies 在 `Plan` 与 `Conversations` 服务装配完成后写入共享的 capability 值，避免 factory 闭包延迟读取 bindings。session 异步工作仍经过 frontend lifecycle admission 和同一 session actor，队列恢复仍在 completion cleanup 后调度；plan-exit follow-up 保留 parent/reuse、message-link 和缺少 Feishu client 时的失败行为。新显式 outbound/link adapter 替代后删除三个已无调用方的 App helper。对照 SM-03/04/06/08/14/25/26：未改变 thread/turn 绑定、终态边界、review/compact/goal continuation 分流、异步问题处理或 turn 完成后的队列顺序。自动重试队列优先级测试继续覆盖 retry 清理后同 session 与 group queue 的恢复。生产 `*App` 引用预算由 280 降至 276，收 `*App` 的顶层函数由 146 降至 142，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 7 降至 6，lazy binding-read 预算保持 0；`TurnPorts` 的 App 传递依赖归零。

步骤 137 将 `BackendFailurePorts(*App)` 改为接收 `BackendFailurePortInputs`，由 composition 显式传入 runtime、turn/compaction、interaction、retry、cleanup、queue、card 与 async owners；failure adapter 不再读取 bindings 或捕获 App。后台任务仍通过 frontend lifecycle admission 和同一 session actor 执行。对照 SM-04/06/07：失败上下文记录不触发终态，仍由 `turn/completed(failed)` 收口；interrupt 与 compaction 生命周期不变。生产 `*App` 引用预算由 276 降至 275，收 `*App` 的顶层函数由 142 降至 141，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 6 降至 5，lazy binding-read 预算保持 0；`BackendFailurePorts` 的 App 传递依赖归零。

步骤 138 将 `CodexRecoveryPorts(*App, ...)` 改为接收 `CodexRecoveryPortInputs`，runtime state、frontend identity/config、scoped session state、lifecycle admission 与 session actors 通过显式 runtime snapshot 提供；submission queue 和异步 runner 也显式注入。BackendFailure 尚在稍后构造，因此 recovery 的失败回调使用 composition 局部 owner 槽位；StartupRecovery callback 使用局部 function 槽位，均不捕获 App 或 bindings 容器。对照 SM-03：只调整恢复依赖装配；恢复完成后先恢复 frontend runtime state，再按原 session actor 顺序启动 queued submission，thread/session 隔离和 client promotion 边界不变。`depmap --bindings` 中的惰性读取 SCC 从 CodexRecovery、ConversationRecovery、MaintenanceCommands、StartupRecovery 四节点降为仅 MaintenanceCommands 与 StartupRecovery 两节点。生产 `*App` 引用预算由 275 降至 274，收 `*App` 的顶层函数由 141 降至 140，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 5 降至 4，lazy binding-read 预算保持 0；`CodexRecoveryPorts` 的 App 传递依赖归零。

步骤 139 将 `TurnPresentationPorts(*App, turns)` 改为 `TurnPresentationPorts(TurnPresentationPortInputs)`，显式传入 runtime snapshot、turn lifecycle、stream/item trackers、submission lookup/status、compaction owner 与共享 outbound cards。开始提示保留原来的 `ClaimStartNotice` 去重、最新 submission 读取、`turn_started` 消息类型和 quiet-mode 过滤；workspace/config 查询仍使用当前 frontend config view。删除 `maybeSendSubmissionStartedNotice(*App, ...)`、其 App-bound sender 包装及由此失去调用方的 `sendTurnEventMessages`。对照 SM-04/07/08：只改变 presentation dependencies；item state、quiet working card 复用及 turn/compaction 终态顺序不变。生产 `*App` 引用预算由 274 降至 270，收 `*App` 的顶层函数由 140 降至 136，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 4 降至 3，lazy binding-read 预算保持 0；`TurnPresentationPorts` 的 App 传递依赖归零。

步骤 140 将 `StartupRecoveryPorts(*App, ...)` 改为接收 runtime snapshot、`StartupState`、cleanup/restore callbacks 与 frontend-scoped effect text sender。backend configured 检查和启动恢复 scope 在执行时查询当前 frontend runtime/config，session filtering、RecoveryMu、live-thread reset、状态重置、attachment cleanup、conversation restore 与 ready notification 顺序不变。对照 SM-03 与启动恢复测试：恢复仍先清空 live-thread 标记，再按当前 backend scope 执行 restore；仅调整 dependencies。删除由迁移失去调用方的 App-bound `sendTextEffect`。生产 `*App` 引用预算由 270 降至 268，收 `*App` 的顶层函数由 136 降至 134，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 3 降至 2，lazy binding-read 预算保持 0；`StartupRecoveryPorts` 的 App 传递依赖归零。

步骤 141 将 `ReviewPorts(*App)` 改为接收 `ReviewPortInputs`，显式传入 runtime snapshot、forms/delivery、frontend context/state、submission queue、pending queue 与 outbound cards；Codex gateway 在调用时继续查询当前 frontend runtime。review target 解析和队列/卡片通知仍由原 owner 执行。对照 SM-14：只改变 review dependencies，不改变 `review/start`、target resolution 或 submission queue 生命周期。生产 `*App` 引用预算由 268 降至 267，收 `*App` 的顶层函数由 134 降至 133，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂由 2 降至 1，lazy binding-read 预算保持 0；`ReviewPorts` 的 App 传递依赖归零。

步骤 142 将 `startMaintenanceRestartFromMessage` 改为接收 frontend config view、线程回复策略与显式 reply port，不再把 `*App` 传入通用维护重启 helper。Claude/Codex 重启仍使用相同的 session key、回复位置和 maintenance service；维护 operation 的开始、运行与失败收口顺序不变。生产 `*App` 引用预算由 267 降至 266，收 `*App` 的顶层函数由 133 降至 132，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 143 删除生产 `newOutboundCardService(*App)` 重建入口，由 composition 将已构造的 outbound owner 放入 `Bindings`，Review 和 Turn 复用同一实例；App 的最终卡片发送适配也从该 owner 取 delivery，而不是重新读取多个 bindings 后拼装。测试 fixture 通过 `_test.go` helper 保留便捷构造。card rendering、message-link recording、final footer 与 effect 投递实现不变。生产 `*App` 引用预算由 266 降至 265，收 `*App` 的顶层函数由 132 降至 131，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 144 将高扇入的 `patchMaintenanceCard(*App, ...)` 改为接收 lifecycle context、frontend ID 与 effect runner。async input、goal、plan、menu 和 backend upgrade 的卡片 patch 仍通过同一 effect runner 执行，日志字段及失败处理不变。生产 `*App` 引用预算由 265 降至 264，收 `*App` 的顶层函数由 131 降至 130，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 145 删除 `completeMaintenanceAsyncAction` 的薄转发，并让 backend upgrade command/card handlers 直接调用当前 service 实例，不再经 `App.Bindings.BackendUpgrades` 回调自身。命令参数、准备卡、失败卡和异步 dispatcher 参数保持一致；maintenance operation 生命周期不变。生产 `*App` 引用预算由 264 降至 263，收 `*App` 的顶层函数由 130 降至 129，App-bearing 结构体保持 4，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0；backend upgrade 的自绑定依赖边归零。

步骤 146 将 `backendUpgradeService` 改为显式持有 session-key/reply policy、effect runner、frontend lifecycle、backend maintenance services/runners、maintenance state、presentation、forms 与 Codex upgrade owners；CardAction adapter 通过命令-completion port 继续调用原异步 command dispatcher。composition 移到 maintenance owners 都就绪后创建该 service。对照状态机审计的 frontend lifecycle admission、context cancellation 与维护流程要求：异步取消刷新仍由同一 lifecycle admission 和 20 秒 operation timeout 执行，restart/upgrade runner 调用顺序不变。生产 `*App` 引用预算由 263 降至 261，收 `*App` 的顶层函数由 129 降至 127，App-bearing 结构体由 4 降至 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0；`backendUpgradeService` 的 App-bearing 依赖闭包归零。

步骤 147 将 `groupBindingSessionScopeActive` 从 `*App` 参数改为已构造的 `bindingSessionScope`，命令、workspace callback 与 help renderer 复用 `BindingCommands` 持有的 state、session-key normalizer、primary service 和 frontend identity。session metadata 回退、group binding 检查与 primary lookup 失败时的非群判定保持不变。生产 `*App` 引用预算由 261 降至 260，depmap 顶层 App-taking 函数计数保持 127，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 148 删除重复的 `sessionKeyChatForApp(*App)`，并让 p2p 判断、workspace thread menu、群配置状态卡和 command bridge 直接复用已构造的 `bindingSessionScope`。session metadata、规范化 session key、存储 session fallback、binding/primary 推断以及有效 group session key 查询保持原顺序和结果。生产 `*App` 引用预算由 260 降至 257，收 `*App` 的顶层函数由 127 降至 124，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 149 将 `threadMenuEffectiveSessionKey` 改为接收 session-key normalizer、`bindingSessionScope` 和 `ConversationQuery`，由 menu renderer、fork command 与 thread-menu adapter 显式组装这些依赖。群 session 选择、binding ID 过滤和 active thread 优先级不变；缺少 normalizer 或 query repository 时仍原样返回 key。生产 `*App` 引用预算由 257 降至 256，收 `*App` 的顶层函数由 124 降至 123，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 150 将 `renderHelpBodyForSession` 改为接收 `bindingSessionScope`，调用方直接从 `BindingCommands` 提供当前 frontend scope。group help 可见性继续使用 session metadata 和 binding/primary 回退判定，不改变命令列表。生产 `*App` 引用预算由 256 降至 255，收 `*App` 的顶层函数由 123 降至 122，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 151 删除 `commandWorkspaceProfileAware`、`renderSessionMenuCard` 和 `renderContextMenuCard` 三个纯转发函数，workspace 命令直接进现有 handler，菜单测试直接验证实际 renderer。命令分流与卡片内容保持不变。生产 `*App` 引用预算由 255 降至 252，收 `*App` 的顶层函数由 122 降至 119，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 152 删除无调用方的 `sendReplyMessages` 包装函数，唯一生产实现继续使用 `sendReplyMessagesWithReuse`，投递、复用消息 ID 和 message-link 记录逻辑不变。生产 `*App` 引用预算由 252 降至 251，收 `*App` 的顶层函数由 119 降至 118，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 153 删除 `replyTextChunkedEffect(*App)` 中重复的分块循环，文本 fallback 改为通过窄 `effectOutbound` port 复用 `replyTextChunked`；同时删除因此失去调用方的 `replyTextWithIDEffect(*App)`。分块、code fence 保留和后续分块失败时报告已送达首段的语义不变。生产 `*App` 引用预算由 251 降至 249，收 `*App` 的顶层函数由 118 降至 116，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 预算保持 0。

步骤 154 将 `enqueuePassthroughCommand` 改为接收 submission queue 与调用方解析的 session key，不再收 `*App`。消息副本、trim 后的 raw command、空输入短路和普通 submission enqueue 参数保持一致；生产 `*App` 引用预算由 249 降至 248，收 `*App` 的顶层函数由 116 降至 115，其他棘轮保持。

步骤 155 删除无生产调用方的 `renderQuietModeCard(*App)` 空 session-key 转发，测试改为直接覆盖 `renderQuietModeMenuCard`。quiet mode 卡片内容和按钮不变；生产 `*App` 引用预算由 248 降至 247，收 `*App` 的顶层函数由 115 降至 114，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持。

步骤 156 删除 `commandPlan(*App)` 纯转发，feature registry 直接调用 `planmode.CommandPlan` 和现有 adapter；Plan 命令参数及错误处理不变。测试通过 `_test.go` helper 调用同一 use case。生产 `*App` 引用预算由 247 降至 246，收 `*App` 的顶层函数由 114 降至 113，其他棘轮保持。

步骤 157 将 MCP 构造从 `BuildMCP(*App)` / `mcpDependenciesForApp(*App)` 改为 `MCPPortInputs`、`MCPPorts` 与 composition 显式组装。MCP state provider 使用 frontend-scoped `Session/Sessions/Submission` API；跨 frontend session 不会进入当前 MCP 服务。Started turn item、submission lookup、附件 sender 和 reply policy 仍由原 owners 提供。生产 `*App` 引用预算由 246 降至 244，收 `*App` 的顶层函数由 113 降至 111，App-bearing 结构体保持 3，App-bearing `*Ports` 工厂保持 1，lazy binding-read 保持 0。

步骤 158 将 19 个 menu-action handler 移到既有 `cardActionService` callback context，所有 production callsites 直接调用当前 handler context，删除重复的 `menuActionService{app:*App}` 和 `newMenuActionService(*App)`。异步 callback、升级命令、卡片渲染与返回内容保持不变。App 引用预算由 244 降至 242，App-taking 函数由 111 降至 110，App-bearing 结构体由 3 降至 2，App-bearing `*Ports` 工厂保持 1，lazy binding-read 保持 0。

步骤 159 将 `commandMessageFromAction` 的状态和会话推断依赖改为显式 `bindingSessionScope`，绑定命令复用 service 已持有的 scope，workspace/review/backend action adapters 在构造时捕获 scope 值。session key 解析、state 回退和 group session 推断顺序不变。生产 `*App` 引用由 242 降至 241，App-taking 顶层函数由 110 降至 109，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 160 将 `updateQuietMode(*App, ...)` 改为接收 `runtimeconfig.Service`，command/card handler 和测试均从现有 RuntimeSettings owner 调用。quiet mode 规范化、持久化及错误返回保持不变。生产 `*App` 引用由 241 降至 240，App-taking 顶层函数由 109 降至 108，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 161 将 `renderQuietModeMenuCard(*App, ...)` 改为接收 quiet mode、最终标题和 card renderer。命令与 callback 在调用处用现有 config/plan/Feishu owners 组装这些输入，选中态、按钮及返回导航保持不变。生产 `*App` 引用由 240 降至 239，App-taking 顶层函数由 108 降至 107，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 162 将 root/tools/system/backend/help 五个菜单 renderer 改为显式接收 backend、标题、Feishu card renderer 与 session scope（help）。菜单 body、按钮、标题和 backend 未配置时的展示保持不变；测试用 `_test.go` helper 继续以 fixture 构造同样的输入。生产 `*App` 引用由 239 降至 234，App-taking 顶层函数由 107 降至 102，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 163 删除 `renderClaudeSessionPermissionMenuCard(*App, ...)` 转发函数及 `App.RenderClaudeSessionPermissionMenuCard` 兼容方法。会话权限 renderer 在 thread-menu composition 中一次构造并注入，命令入口直接用显式 config/backend/session 输入构造 renderer；permission options 与展示保持不变。生产 `*App` 引用由 234 降至 232，App-taking 顶层函数由 102 降至 101，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 164 删除没有调用方的 `App.PlanModeTitleForSession` 和 `App.ContentCardTitleForSession` 兼容方法。生产调用继续使用现有纯函数与 state-based title renderer；标题生成逻辑不变。生产 `*App` 引用由 232 降至 230，收 `*App` 的顶层函数由 101 降至 99，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 165 将 plan/content card title helpers 改为接收 frontend state provider 与 config-enabled 标志；调用方使用已持有的 scoped state。nil App 时 config-enabled 仍为 false，title adapter 的空配置行为不变。生产 `*App` 引用由 230 降至 227，收 `*App` 的顶层函数由 99 降至 96，App-bearing 结构体、App-bearing `*Ports` 工厂及 lazy binding-read 预算保持。

步骤 166 将 workspace management 的 binding session scope 改为显式 `BindingScope` 输入。composition 从 frontend state、session-key normalizer、Primary owner 和 frontend ID 构造 scope，并在 `WorkspaceManagement` 后构造 `BindingCommands`；workspace service 不再反向读取 `BindingCommands.scope`。这清除 BindingCommands/WorkspaceManagement 的构造反向边，为后续注入 BindingCommands 所需 owners 留出 DAG 顺序。命令、卡片 callback 和 session 推断行为不变；`*App` 引用预算保持 227、收 `*App` 的顶层函数保持 96、App-bearing 结构体保持 2、App-bearing `*Ports` 工厂保持 1，lazy binding-read 保持 0。

步骤 167 将 `bindingService` 改为接收 `BindingCommandInputs`：config/state/session scope、routing/model/workspace owners、Feishu card renderer、effect runner、异步 lifecycle admission 与 group-primary callbacks 均由 composition 显式提供。`bindingService` 不再持有 App；workspace manager 的反向构造边已拆，因此 binding owner 在依赖就绪后构造。群绑定、model 菜单和 clone/worktree 回调仍委托相同的 application/runtime owners，异步任务仍经过 frontend lifecycle admission。生产 `*App` 引用由 227 降至 225，收 `*App` 的顶层函数由 96 降至 95，App-bearing 结构体由 2 降至 1，App-bearing `*Ports` 工厂保持 1，lazy binding-read 保持 0。

步骤 168 将 path-picker 的 dropdown/up/open/select/confirm/cancel callbacks 从 App-bound workspace handler map 移入独立的 `pathPickerActionService`，由 composition 显式提供 scoped state、forms、picker、workspace planning/presentation、upgrade/debug owners 和 status-card renderer。下载确认委托给既有 Debug owner；测试 fixture 仍通过 test-only wrapper 复用原场景。path validation、pending owner 检查、draft/status 写入及确认分支不变；不涉及 app-server lifecycle。生产 `*App` 引用由 225 降至 224，收 `*App` 的顶层函数由 95 降至 94，App-bearing 结构体保持 1，App-bearing `*Ports` 工厂保持 1，lazy binding-read 保持 0。

步骤 169 删除 `commandCompact(*App, ...)` 与 `runMenuCompactAction(*App, ...)` 两个薄桥接。compact command handler 直接从当前 frontend 取得 `BackendActions` 与 `Compaction` owners，菜单 callback 也直接调用这两个 owners；参数校验与 compact RPC/通知行为不变。生产 `*App` 引用由 224 降至 222，收 `*App` 的顶层函数由 94 降至 92，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持。

步骤 170 将 `pending_form.plan_reject` 移入 App-free pending handler map，显式注入 Claude support 与 server-request owners。拒绝仍先调用对应 backend adapter 的 `CancelPending`，成功后再 finalize pending 并渲染原状态卡；owner 校验、失败 toast 与 pending 状态边界不变。对照状态机审计的 pending interaction 约束，本次仅改变依赖传递。生产 `*App` 引用由 222 降至 221，收 `*App` 的顶层函数由 92 降至 91，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持。

步骤 171 将 `menu.interrupt`、`menu.thread` 和 `menu.new` 从 feature 生成的 App-bound handler map 移入 `ThreadMenu` owner map；同一 `menu.thread` feature 的 `menu.fork` 仍留在 App-bound map。菜单结构、action name 和顺序不变。对照 SM-06，中断仍只发起请求，turn 终态仍由 `turn/completed(interrupted)` 收口；本次仅改 callback owner 的传递。`*App` 文本引用、App-taking 函数及 lazy binding-read 预算保持 221/91/0，因为其它 callback 仍依赖 `cardActionService`。

步骤 172 将 `completeAsyncUserInput` 改为显式接收 scoped state、`asyncinput.Service`、frontend context/identity、effect runner 与 status-card renderer；answer/cancel callbacks 移入 owner-bound action map。同步校验、原子 claim、快速 ack、lifecycle admission、active turn 时 steer、空闲时同 thread queue、后端接受后 resolve、失败恢复 pending 草稿及 cancel 不打断 turn 的顺序均不变。对照 SM-26：async question 仍是本地 pending form，不创建 server request reply/resolved 边界。生产 `*App` 引用由 221 降至 220，收 `*App` 的顶层函数由 91 降至 90，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 173 将 `pending_form.cancel` 从 App-bound handler 移至显式 `PendingFormCancelActionInputs`，由 composition 注入 scoped state、server-request service、pending finalizer 和对应 renderer。server-request 所有的 pending 仍交由原 service 完成；root workspace/review form 仍先 finalize 再返回原卡片，Claude plan cancel 仍先调用 backend cancel，成功后才 finalize。对照 SM-09/11/23 的 reply/resolved 边界，本次不改变 protocol 完成路径。生产 `*App` 引用由 220 降至 218，收 `*App` 的顶层函数由 90 降至 88，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 174 将 Codex plan-exit 的 implement-current、implement-fresh、stay callbacks 移入显式 `planmode.Dependencies` handler map，并删除仅转发到 `PlanModePorts` 的 `completeCodexPlanModeExit(*App)`。composition 复用同时交给 turn lifecycle 的同一份 plan-mode capability；plan exit 的 pending 校验、stay 分支、fresh/current 实现、async lifecycle admission、follow-up card 与 active-turn steer 顺序仍由原 `planmode.CompleteCodexPlanModeExit` 处理。对照审计中的 plan item 与 turn completion 边界，本次只改变依赖传递。生产 `*App` 引用由 218 降至 217，收 `*App` 的顶层函数由 88 降至 87，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 175 删除只转出 `Bindings.ServerRequests` 的 `App.ServerRequestService()` 兼容 accessor。Claude support 在构造时捕获已就绪的 server-request owner，pending cancel 仍调用相同 backend adapter；测试 fixture 直接访问绑定的 owner。对照 SM-09/10/11/22/23，本次不改变 reply/resolved 或 pending 状态边界。生产 `*App` 引用由 217 降至 216，收 `*App` 的顶层函数由 87 降至 86，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 176 删除无生产调用方的 `App.Claude()`、`App.Codex()` 与 `App.BackendRuntime()` facade；composition 的 goal gateway 直接从已持有的 frontend runtime owner 获取当前 Codex client，测试 fixture 和断言直接读取已有 `runtimeView`。owner 的 client 来源与 `App.Codex()` 相同。生产 `*App` 引用由 216 降至 213，收 `*App` 的顶层函数由 86 降至 83，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 177 删除无生产调用方的 `App.AutoRetries()`、`App.RunAsync()`、`App.MenuCardBody()`、`App.SessionHasActiveWork()` 与 `App.LockAutoRetryDispatch()` facade。ThreadMenu 在构造期直接取 auto-retry tracker 的 `LockDispatch` 方法值；auto-retry executor 显式通过 frontend lifecycle admission 后再交给 async runner，关闭期间拒绝新任务的行为保持不变。生产 `*App` 引用由 213 降至 208，收 `*App` 的顶层函数由 83 降至 78，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 178 删除无生产调用方的 `App.ActionStringValue()` 和 `App.MenuCardBodyForBackend()` 转发 facade。goal、planmode、threadmenu 以及菜单 renderer 已直接使用相应纯函数，生产 `*App` 引用由 208 降至 206，收 `*App` 的顶层函数由 78 降至 76，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 179 将 thread menu 的 config provider、frontend state、effective session key、conversation/runtime、pending queue、workspace 与 backend action capabilities 改为构造期显式值；生命周期 context 直接从 frontend owner 读取，避免 `BackendRuntimeDeps` 默认携带的 `App.Context` 方法值。configured backend builder 仍在调用时读取当前 frontend/backend 配置，未把 backend 选择固化为快照。自动重试取消改为直接调用既有 retry engine，并删除对应 App facade。对照 SM-03/04/06：thread resume、active turn reconcile 与 interrupt 请求/终态收口顺序不变，仅改变 dependency assembly；`CommandFork`、通用菜单命令与 Claude 权限菜单的 App-bound 路由回调仍待后续拆分。生产 `*App` 引用由 206 降至 196，收 `*App` 的函数数由 76 降至 75，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 180 将 `/fork`、`/thread fork` 与 `/session fork` 共用的 fork command 改为显式接收 scoped session repository、动态 backend/config providers、pending queue、conversation service 和 effect runner。thread menu service 暴露 `CommandFork`，feature command 直接路由到该 owner；fork 前的 active-work 检查、workspace 解析、queue discard、fork 后 session 刷新及回复顺序不变。对照 SM-03：仍由同一 conversation service 发起 `thread/fork` 并使用 RPC 返回的 thread 更新 session；本次不改变 SM-04/06 lifecycle。生产 `*App` 引用由 196 降至 193，收 `*App` 的函数数由 75 降至 72，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

步骤 181 删除没有调用点的 `ShowClaudeSessionPermissionMenuFromApp` callback、App facade 与发送 helper；session permission renderer 保持由 thread menu 正常使用。生产 `*App` 引用由 193 降至 191，收 `*App` 的函数数由 72 降至 70，App-bearing 结构体、App-bearing `*Ports` 工厂和 lazy binding-read 预算保持 1/1/0。

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
