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
| `internal/feishuapp` 生产代码里的 `*App` 引用 | 582 |
| 收 `*App` 的顶层函数 | 376 |
| `*Ports` 工厂 | 29 |
| 工厂传递依赖的不同 `*App` helper | 108 |
| `Bindings` 字段 | 104 |

工具（`scripts/apprewrite`）只能做参数替换；工厂改造是**逐个判断**的活，
因为每个闭包要单独决定捕获哪些窄值。所以下面按"先改 helper、再改工厂"
排序，而不是按文件顺序。

## 施工顺序

### 第一阶段：按扇入改 helper

下面这些 helper 被最多工厂（传递地）依赖，先改它们，每个能解锁若干工厂。
括号里是依赖它的工厂数。

| helper | 工厂数 | 当前形态 | 需要的输入 |
|---|---|---|---|
| `ensureRuntimeOwner` | 18 | `return a.runtimeOwner` | `*frontendruntime.FrontendOwner` |
| `feishuConfigUnlocked` | 15 | 用 `Config()` + `FrontendConfigIndex()` | `*config.Config, int` |
| `configuredBackend` | 14 | 用 `Backend()` + `ConfigMu()` | 同上 |
| `getCodex` | 13 | — | — |
| `currentCodexClient` | 9 | — | — |
| `normalizeSessionKey` | 8 | 用 `FrontendID()` | `string` |
| `requireCodexClient` | 8 | — | — |
| `runAsync` | 7 | 用 `asyncRunner` | `func(func())` |
| `currentClaudeCore` | 7 | — | — |
| `currentMCPPublication` | 6 | 用 `runtimeOwner` + `bindings.MCP` | 二者 |
| `defaultWorkspaceID` | 6 | 用 `Config()` + `ConfigMu()` | 同上 |
| `sessionKeyForBackendEvent` | 6 | 用 `bindings.ConversationQuery` | 该服务 |
| `failBackendActiveWork` | 6 | 用 `store` + `bindings.BackendFailure` | 二者 |
| `makeSessionKey` | 6 | 用 `FrontendID()` | `string` |

这些函数的共同点：`direct` 和 `bindings` 加起来只有 1-2 项，转换是机械的。
`ensureRuntimeOwner` 甚至只是返回一个字段，可以直接内联掉。

### 第二阶段：按传递依赖从小到大改工厂

依赖规模（传递触达的 helper 数）决定难度：

| 传递依赖数 | 工厂 |
|---|---|
| 0 | `BackendMaintenancePorts`, `ConversationControlPorts`, `SkillUseCasePorts`, `UpgradeWorkflowPorts`, `WorkspaceCreationPorts` |
| 1 | `BackendEventPorts`, `CardActionPorts`, `PendingQueuePorts` |
| 2-5 | `BackendSwitchPorts`, `GoalContinuationPorts`, `CompactionPorts`, `InboundPorts`, `PlanPorts`, `ReviewPorts` |
| 6-11 | `ClaudeMaintenancePorts`, `ConversationRecoveryPorts`, `ConversationPorts`, `BackendFailurePorts`, `ContinuationPorts`, `CodexRecoveryPorts` |
| 15-29 | `CodexUpgradePorts`, `TurnPorts`, `AutoRetryPorts`, `StartupRecoveryPorts`, `TurnPresentationPorts` |
| 36-51 | `FileSharePorts`, `GoalCommandPorts`, `SubmissionPorts`, `ClaudeRuntimePorts` |

第一阶段做完后，这张表里的数字会普遍下降，届时需要重新生成（见下）。

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
