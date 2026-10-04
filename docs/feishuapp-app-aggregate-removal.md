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
| `internal/feishuapp` 生产代码里的 `*App` 引用 | 516 |
| 收 `*App` 的顶层函数 | 314 |
| 收 `*App` 的 `*Ports` 工厂 | 22 |
| **持有 `*App` 字段的结构体** | **70** |

棘轮只有一个方向：任何一次提交都不许让这些数字变大。惰性读取另有单独的
预算（`TestFeishuAppLazyBindingReadsDoesNotGrow`，见构造环文档），当前 38。

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

### 修正：扇入数的是"碰过它的工厂"，不是"转换它能解放的工厂"

`cardRenderer` 被 6 个工厂依赖，按扇入是最高杠杆点。转换之后——6 个工厂
的依赖里它确实都消失了，**但没有一个工厂自由**，因为它们各自还依赖 2-7 个
别的结构体。工厂只有在**全部**结构体依赖都转换后才会自由。

正确的排序指标是「**哪些结构体是某个工厂的最后一个阻塞点**」：

| 解放工厂数 | 结构体 |
|---|---|
| **2** | `sqLiveThreadAdapter` |
| 1 | `backendInteractionPresenter` |
| 1 | `backendSelectionRuntime` |
| 1 | `cardActionService` |
| 1 | `claudeTurnStreamPort` |
| 1 | `conversationRuntimeControl` |

以及「已经只剩 helper、没有结构体依赖」的 5 个工厂：

`CodexUpgradePorts`(0 helper)、`ConversationRecoveryPorts`(0)、
`ClaudeMaintenancePorts`(1)、
`StartupRecoveryPorts`(3)、`CodexRecoveryPorts`(6)。

### 第一优先：按结构体扇入施工

结构体（而不是工厂）才是依赖单元。扇入最高的：

| 工厂数 | 结构体 | 方法数 | 需转换的 helper |
|---|---|---|---|
| 6 | `cardRenderer` | 5 | 1 |
| 3 | `sqLiveThreadAdapter` | 3 | 3 |
| 3 | `outboundCardService` | 7 | 12 |
| 3 | `pendingCardPresenter` | 1 | 3 |
| 2 | `menuActionService` | 21 | 15 |
| 2 | `goalOutbound` | 3 | 3 |
| 2 | `turnRuntimePort` | 3 | 2 |
| 2 | `planModeOutbound` | 4 | 4 |

`cardRenderer` 是最高杠杆点：改一个结构体解锁 6 个工厂。

### 第二优先：工厂按"自身成员 + 传递 helper + 结构体"排序

| 合计 | 工厂 | 自身成员 | 传递 helper | 结构体 |
|---|---|---|---|---|
| 3 | `CardActionPorts` | 2 | 0 | 1 |
| 5 | `BackendMaintenancePorts` | 2 | 1 | 2 |
| 5 | `ClaudeMaintenancePorts` | 4 | 1 | 0 |
| 6 | `ConversationRecoveryPorts` | 6 | 0 | 0 |
| 7 | `CodexUpgradePorts` | 7 | 0 | 0 |
| 10 | `ConversationControlPorts` | 7 | 2 | 1 |
| 12 | `BackendEventPorts` | 7 | 4 | 1 |
| 12 | `StartupRecoveryPorts` | 9 | 3 | 0 |
| 14 | `CodexRecoveryPorts` | 8 | 6 | 0 |
| 15 | `ConversationPorts` | 9 | 5 | 1 |
| 16 | `GoalContinuationPorts` | 6 | 8 | 2 |
| 17 | `BackendSwitchPorts` | 2 | 14 | 1 |
| 17 | `AutoRetryPorts` | 9 | 7 | 1 |
| 33 | `InboundPorts` | 8 | 19 | 6 |
| 34 | `ReviewPorts` | 3 | 27 | 4 |
| 42 | `GoalCommandPorts` | 0 | 39 | 3 |
| 48 | `FileSharePorts` | 2 | 38 | 8 |
| 50 | `BackendFailurePorts` | 10 | 37 | 3 |
| 51 | `TurnPresentationPorts` | 8 | 38 | 5 |
| 60 | `ClaudeRuntimePorts` | 13 | 45 | 2 |
| 63 | `TurnPorts` | 11 | 45 | 7 |
| 71 | `SubmissionPorts` | 19 | 46 | 6 |

注意 `BackendSwitchPorts`：自身只碰 2 个成员，但传递依赖 14 个 helper + 1
个结构体 —— 先做过一轮，撞墙了才明白瓶颈不在工厂而在
`backendSelectionRuntime` 的方法链。

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

前两个是 29 个里仅有的**立即求值、不捕获**的工厂。步骤 3-5 走的是同一
条路：值在调用时已经就绪，惰性读取纯属写法惯性。

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
