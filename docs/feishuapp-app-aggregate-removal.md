# 拆掉 `feishuapp` 的 `*App` 聚合

本文是施工图，记录把 `internal/feishuapp` 里 582 处 `*App` 引用清掉的路径、
已完成的步骤，以及下一步该改什么、为什么按这个顺序。

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

## 当前状态

| 指标 | 值 |
|---|---|
| `internal/feishuapp` 生产代码里的 `*App` 引用 | 543 |
| 收 `*App` 的顶层函数 | 350 |
| `*Ports` 工厂 | 25 |
| **持有 `*App` 字段的结构体** | **72** |

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
| 1 | `planSettingsSource` |

以及「已经只剩 helper、没有结构体依赖」的 7 个工厂：

`CodexUpgradePorts`(0 helper)、`ConversationRecoveryPorts`(0)、
`ClaudeMaintenancePorts`(1)、`CompactionPorts`(1)、`ContinuationPorts`(1)、
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
| 3 | `PlanPorts` | 1 | 1 | 1 |
| 3 | `CardActionPorts` | 2 | 0 | 1 |
| 5 | `BackendMaintenancePorts` | 2 | 1 | 2 |
| 5 | `ClaudeMaintenancePorts` | 4 | 1 | 0 |
| 5 | `CompactionPorts` | 4 | 1 | 0 |
| 6 | `ConversationRecoveryPorts` | 6 | 0 | 0 |
| 7 | `CodexUpgradePorts` | 7 | 0 | 0 |
| 8 | `ContinuationPorts` | 7 | 1 | 0 |
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

这两个是 29 个里仅有的**立即求值、不捕获**的工厂，所以能一次改完。
其余 27 个都在闭包或返回结构体里捕获 `app`。

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
