# 拆掉 `feishuapp` 的构造环

本文记录 `internal/composition` 构造 `feishuapp.Bindings` 时存在的两个真环，
以及把它们拆成 DAG 的设计。配套文档：
[feishuapp `*App` 聚合移除](feishuapp-app-aggregate-removal.md)。

## 为什么需要拆

`BackendRuntimeDeps` 是**快照**语义：`App.BackendRuntimeDeps()` 在调用时把字段抄
走。所以它只能在被依赖的值都已经赋值之后调用。

而 composition 现在是"边构造边填字段"：工厂在 `bindings` 还没填完时就被调用，
闭包里用 `a.bindings.X` **惰性读取**（调用时才读，那时已赋值）才能工作。换成快照
的 bundle 就会读到 nil —— 这正是 `CodexUpgradePorts` 第一次改造时三个测试挂掉的
原因。

把构造顺序做成拓扑排序能解决**大部分**工厂，但排序对真环无效。所以先要把环拆掉。

## 环的形状

判定方法：把 composition 里每条 `bindings.X = ...` 赋值语句作为节点，语句里
调用的 `feishuapp.*` 函数所读取的其他 binding 作为出边，求强连通分量。

**有 3 个真环**（其余是排序问题）：

```
环 1:  Submissions ↔ TurnPresentation ↔ Turns
环 2:  CodexRecovery ↔ CodexUpgrade ↔ ConversationRecovery ↔ MaintenanceCommands ↔ StartupRecovery
环 3:  Inbound ↔ ForwardInputs
```

边（行号为本文件写作时的 `internal/composition/app.go`）：

| 语句 | 由谁构造 | 读环内 |
|---|---|---|
| `Submissions` (194) | `SubmissionPorts` | TurnPresentation |
| `TurnPresentation` (196) | `TurnPresentationPorts` | Turns |
| `Turns` (195) | `TurnPorts` | Submissions, TurnPresentation |
| `CodexRecovery` (157) | `CodexRecoveryPorts` | CodexUpgrade, StartupRecovery（另读 Submissions） |
| `CodexUpgrade` (156) | `CodexUpgradePorts` | CodexRecovery, StartupRecovery（另**自引用**） |
| `ConversationRecovery` (212) | `ConversationRecoveryPorts` | CodexRecovery |
| `MaintenanceCommands` (151) | `BuildMaintenanceCommands` | StartupRecovery |
| `StartupRecovery` (150) | `StartupRecoveryPorts` | ConversationRecovery, MaintenanceCommands |
| `Inbound` (202) | `InboundPorts` | ForwardInputs |
| `ForwardInputs` (203) | `ForwardFailure`/`ForwardGateway`/`ForwardProcessor`/`ForwardTasks` | Inbound |

### 分析口径上的两次教训

第一版只用"名字以 `*Ports` 结尾"来识别工厂，漏掉了
`BuildMaintenanceCommands` 这类名字不符的，于是漏掉了
`StartupRecovery ↔ MaintenanceCommands`；也漏掉了 `ForwardInputs`（它由四个
`Forward*` 函数拼装，一个 `Ports` 都没有），于是整个环 3 没被发现。

**正确的口径是"这条语句调用了哪些 `feishuapp.*` 函数"，而不是"函数叫什么名字"。**
按名字匹配的统计已经在本迁移里错了三次（`*Ports` 扇入、`cardRenderer` 杠杆、
这次的两个环），改动前务必用语句级口径复核。

另一个细节：`Submissions`、`Turns` 等在初始字面量里已用
`&submission.SubmissionQueueService{}` 预建空占位符，之后用
`*bindings.Submissions = ...` 原地填充。所以"指针已存在"不等于"值已就绪"，
判断向后读要看**值**写入的位置。

## 全量依赖图（2026-10 实测）

`scripts/depmap --bindings` 会按**语句级**口径建图：composition 里每条
`bindings.X = ...` 赋值语句作为节点，它调用的**所有** `feishuapp.*` 函数（不限
`*Ports` 命名）读取的 binding 作为出边，并且**区分 eager 与 lazy**：

- **eager**：`a.bindings.Y` 在函数体里直接读 —— 工厂被调用时就发生，**构成构造
  顺序约束**
- **lazy**：`a.bindings.Y` 在 func literal 里读 —— 闭包运行时才发生，**不构成构造
  顺序约束**，但会挡住快照式的能力包

实测结果（2026-10-04，93 条赋值语句，337 个收 `*App` 的顶层函数、226 个结构体
方法）：

| | 数量 | 环 |
|---|---|---|
| **构造期环（只算 eager）** | **0** | —— |
| 含惰性读取的环 | 1 | recovery 组：`CodexRecovery ↔ CodexUpgrade ↔ ConversationRecovery ↔ MaintenanceCommands ↔ StartupRecovery` |

（写作时的 1 个 eager 环 `Plan ↔ Submissions ↔ TurnPresentation ↔ Turns` 已由
`db4a1c2` 拆掉。）

### 已经解决的两个

**环 3（`Inbound ↔ ForwardInputs`）** 和 **环 2（recovery 组）** 都已经通过注入
入口函数值解决：它们的 eager 依赖现在全空，剩下的边全部是惰性的。

逐个看环 2 的边就很清楚（2026-10-04 实测，闭包修正后的口径）：

```
CodexRecovery        eager→[]                  lazy→[CodexUpgrade StartupRecovery]
CodexUpgrade         eager→[StartupRecovery]   lazy→[CodexRecovery]
ConversationRecovery eager→[CodexRecovery]     lazy→[]
StartupRecovery      eager→[]                  lazy→[ConversationRecovery MaintenanceCommands]
MaintenanceCommands  eager→[StartupRecovery]   lazy→[]
```

环内所有 eager 边都是**正向**的（被读的 binding 更早赋值），所以构造期无环；
把 eager 边一起算进去才成一个环，因为反向的那些全是惰性读取 —— 注入的闭包在
composition 那一侧仍然读 `bindings.Z`，但那是延迟读取，不约束顺序。
**判断环时必须区分 eager 与 lazy，否则会把已解决的当成未解决。**

### 当时唯一剩下的真环（已解决）

```
Plan             eager→[Submissions]
Submissions      eager→[Plan, TurnPresentation]
TurnPresentation eager→[Turns]
Turns            eager→[Submissions, TurnPresentation]
```

4 个节点、8 条 eager 边，2026-10 由 `db4a1c2` 拆掉（抽出共享状态载体）；
`TurnStarter` 因为只有惰性边，当时就已经掉出环外。现在构造期环为 0。

## 环 1：turn / submission / turnstream

### 边（方法级）

| 从 | 到 | 调用点 | 要什么 |
|---|---|---|---|
| `turnstream` | `turn` | `adapter/feishu/turnstream/service.go:208`、`:251` → `deps.Lifecycle.BindPendingSubmissionTurn(threadID, turnID, true)` | 把 pending submission 绑到 turn |
| `turn` | `turnstream` | `application/turn/service.go:171` 的 `BindPendingSubmissionTurn` 末尾 → `w.deps.Streams.NoteTurnStarted(sessionKey, sub)` | 确保该 turn 的 stream 存在 |
| `submission` | `turnstream` | `submission_bindings.go:176` `TurnStream: a.bindings.TurnPresentation`（`QueueTurnStreamProvider`） | `NoteTurnStarted`、`DeleteTurnStream` |
| `turn` | `submission` | `turn_lifecycle.go:70` `Queue: app.bindings.Submissions`（`QueueProvider`） | `FindSubmissionByTurn`、`NextQueuedSessionKey`、`StartNextSubmissionAsync` |

`turnstream → turn → turnstream` 是**运行时真的会走的相互递归**，不是构造顺序问题。

### 共享的是什么

四个操作全是**同一个事务的不同片段**：turn 开始时，submission ↔ turn ↔ session
的绑定与它 stream 的建立必须一致。

- `turn.BindPendingSubmissionTurn`（`application/turn/service.go:171`，30 行）：
  绑定 session/submission、`MarkSubmissionRunning`、写 source links、标记 live，
  **然后调出去**确保 stream
- `turnstream.NoteTurnStarted`（`adapter/feishu/turnstream/service.go:159`）：
  发开始通知、加锁、`ensureStreamLocked`

两边各自持有事务的一半，所以要互相调用。

### 拆法：抽出 `turnstart` 用例

新建一个用例（建议放 `internal/application/turn` 或独立的
`internal/application/turnstart`），同时持有两边的状态载体：

- `TurnBindings` tracker（来自 `runtime.FrontendOwner`）
- `TurnStreams` tracker（同上）
- `appstate.Store`

由它实现完整事务：

```go
// 伪代码
func (s Starter) Bind(sessionKey string, sub *Submission, threadID, turnID string, allowReview bool) bool {
    // 原 BindPendingSubmissionTurn 的正文
    ...
    // 不再调用 turnstream.Service.NotifyTurnStarted，而是直接操作 tracker：
    ensureTurnStream(s.streams, sessionKey, sub)   // 见下
    s.sendStartedNotice(sub)                        // 通知属于 presentation，见下
    return true
}
```

拆完的边：

```
turnstream   →  turnstart.Bind(...)          （不再调 turn）
turn.Service →  turnstart.Bind(...)          （或删掉这个转发方法）
turnstart    →  EnsureStreamLocked(tracker)  （直接操作 tracker）
```

**可行性关键**：`turnstream.ensureStreamLocked`
（`adapter/feishu/turnstream/service.go:672`）已经是 3 行转发，真正实现在公开的
`EnsureStreamLocked` 上。所以 `turnstart` 可以直接调用 tracker 层，不需要把逻辑
从 turnstream 里挖出来。

### 需要决定的两点

1. **开始通知归谁**：`NoteTurnStarted` 里的 `svc.deps.SendStartedNotice(ctx, sub)`
   是 presentation 副作用。建议由 `turnstart` 产生一个 effect，交给
   runtime/presentation 执行，而不是让用例直接发送。
2. **`QueueProvider` 那三个方法**：`FindSubmissionByTurn`、
   `NextQueuedSessionKey` 是纯查询，可以直接读 store/tracker；
   `StartNextSubmissionAsync` 是**工作流**不是查询，它才是 `turn → submission`
   这条业务边的正当理由 —— 建议保留，但改成通过一个显式的
   `SubmissionStarter` port，而不是拿整个 `Submissions` 服务。

### 协议约束

这一改动移动的是 **turn 绑定的时序**（谁先加锁、谁先写 store、谁先发通知），
按 `AGENTS.md` 必须逐条对照
[codex-app-server-state-machine-audit.md](codex-app-server-state-machine-audit.md)，
并补状态机测试。至少要覆盖：

- turn 开始时的绑定顺序与 `MarkSubmissionRunning` 的先后
- `allowReview` 两个取值下的行为（两处调用点分别传 `true`）
- 恢复路径（startup recovery 期间 turn 绑定的重放）
- 通知发送失败时的状态一致性

## 环 2：recovery / upgrade 五个服务

### 边（方法级）

| 从 | 到 | 调用点 | 要什么 |
|---|---|---|---|
| `CodexRecovery` | `CodexUpgrade` | `codex_runtime_recovery.go` 的 `CodexRecoveryPorts` → `a.bindings.CodexUpgrade.StartVerifiedCodexClient(ctx)` | 启动时拿一个已验证 client |
| `CodexRecovery` | `StartupRecovery` | 同上 → `recoverFrontendRuntimeState(a.bindings.StartupRecovery)` | 恢复前端运行时状态 |
| `CodexUpgrade` | `CodexUpgrade` | `codex_upgrade_runtime.go` 的 `CodexUpgradePorts` → `a.bindings.CodexUpgrade.CodexSmokeTest(ctx)` | **自引用** |
| `CodexUpgrade` | `CodexRecovery` | 同上 → `replaceCodexClient(a.bindings.CodexRecovery, next)` | 替换 client |
| `CodexUpgrade` | `StartupRecovery` | 同上 → `recoverFrontendRuntimeState(a.bindings.StartupRecovery)` | 同上 |
| `ConversationRecovery` | `CodexRecovery` | `conversation_services.go` 的 `ConversationRecoveryPorts` → `codexRuntimeRecovering(a.bindings.CodexRecovery)` | **只查一个状态** |
| `StartupRecovery` | `ConversationRecovery` | `maintenance_bindings.go` 的 `StartupRecoveryPorts` → `a.bindings.ConversationRecovery.Restore()` | 恢复会话 |
| `StartupRecovery` | `MaintenanceCommands` | 同上 → `a.bindings.MaintenanceCommands`（`StartupState`） | 启动状态 |
| `MaintenanceCommands` | `StartupRecovery` | `maintenance_bindings.go` 的 `BuildMaintenanceCommands` → `a.bindings.StartupRecovery` | 恢复前端运行时状态 |

### 共享的是什么

- `recoverFrontendRuntimeState`（`startup_recovery_bindings.go:12`）是**一行转发**到
  `startuprecovery.RecoverFrontendRuntimeState()`。所以那两条边实际只需要一个
  `maintenance.StartupRecovery` 值，不需要整个 binding。
- `codexRuntimeRecovering(CodexRecovery)` 是**纯状态查询**（"codex 正在恢复吗"）。
  `ConversationRecovery` 为了这一个 bool 持有了整个 RecoverService。

### 拆法

1. **抽出恢复状态载体**：把"是否正在恢复 + 当前 client"做成一个小类型
   （可放 `internal/runtime/codex`），由 `CodexRecovery` 写、
   由 `ConversationRecovery` 和 `CodexUpgrade` 读。
   `ConversationRecovery → CodexRecovery` 这条边消失。
2. **`CodexUpgrade` 自引用**：`SmokeTest` 是 UpgradeService 自己的方法，却被自己的
   port 调用。两种改法：
   - 把 smoke test 移出 service，成为 port 的一个函数值，由 composition 在
     `NewUpgradeService` 之后注入；
   - 或者让 composition 用局部变量后置绑定：
     `var up *UpgradeService; ports := Ports(..., func() *UpgradeService { return up }); up = NewUpgradeService(ports)`
     这也是自引用唯一无法靠重排解决的地方。
3. **`StartupRecovery` 两条边**：把 `RecoverFrontendRuntimeState` 所需的输入
   （目前是 `maintenance.StartupRecovery` 一个值）作为参数传，而不是传整个
   binding。这样 `CodexRecovery`/`CodexUpgrade` 依赖的是那个值。

拆完后剩下的边是有向的：

```
StartupRecovery → ConversationRecovery → (codex 恢复状态)
CodexUpgrade    → (codex 恢复状态), StartupRecovery 的值
CodexRecovery   → CodexUpgrade.StartVerifiedCodexClient   ← 这条要单独看
```

`CodexRecovery → CodexUpgrade` 是**唯一有业务理由的边**（启动需要一个已验证的
client）。建议把"启动已验证 client"抽成一个独立的 port，由 composition 注入，
而不是让 recovery 依赖整个 upgrade 服务。

## 环 3：Inbound / ForwardInputs

只有两条边，但这是最容易被漏掉的一个 —— `ForwardInputs` 由四个 `Forward*`
函数拼装，一个 `*Ports` 都没有，按名字筛选的工具完全看不到它。

| 从 | 到 | 调用点 |
|---|---|---|
| `Inbound` | `ForwardInputs` | `InboundPorts` → `a.bindings.ForwardInputs` |
| `ForwardInputs` | `Inbound` | `ForwardFailure`/`ForwardGateway`/`ForwardProcessor`/`ForwardTasks` → `a.bindings.Inbound` |

### 拆法

先看两边各自要什么：`InboundPorts` 读 `ForwardInputs` 是把它当作**转发处理器**；
`Forward*` 读 `Inbound` 是为了**投递/派发**。和环 1 一样，互相要的是对方身上
那部分状态/能力，而不是整条业务链。建议把 `Forward*` 真正需要的那个能力
（投递入口）抽成一个显式 port，由 composition 注入，两边都不再持有对方的服务。

## 惰性读：也是依赖没整理清

拆环过程中发现，环消失之后**依赖并没有变得清晰**，只是从"构造期环"变成了"惰性读"
—— 注入的闭包把依赖藏进了运行时。顺着这条线做了统计：

| 类别 | 数量 | 含义 |
|---|---|---|
| 值早已就绪、惰性纯属多余 | 14 | 依赖被闭包藏起来，类型系统和静态分析都看不见 |
| 值确实在后面、惰性必需 | 5 | 真正的晚绑定 |

**74% 的惰性读不是语义需要，而是写法惯性。**

### 提升惰性读会立刻暴露真实的顺序违规

把读从闭包提到构造期后，三次里**两次立刻挂测试**：

| 提升处 | 暴露的问题 |
|---|---|
| `SubmissionPorts` 读 `Review` | 测试 fixture 在 `SubmissionPorts` 之后才赋值 `Review`，生产 composition 是之前 —— **两个构造入口的顺序本来就不一致** |
| `BuildUpgrades` 读 `WorkspaceConfiguration` | 同上，且把 fixture 的顺序改对之后又冒出 `BuildWorkspaceConfiguration` 自身的第二个 nil |

结论：**惰性读在替不一致的构造顺序兜底**，把本该校验出来的错误推迟成运行时的
nil。这也解释了为什么 `SubmissionPorts` 那处能改、而 `BuildUpgrades` 那处不行
—— 后者的两个入口还没对齐。

### 已提升的与待处理的

已提升 7 处（`FrontendFacts`、`CodexUpgradePorts`、`BuildClaudeSupport`、
`BuildServerRequests`×2、`ConversationRecoveryPorts`×2，以及更早的
`SubmissionPorts`×4）。

**待处理**：

- `BuildUpgrades ← WorkspacePresentation`、`CodexUpgradePorts ← CodexRecovery`
  —— 值确实在后面，需要先拆掉它俩的依赖（后者是环 2 注入的残留，正解是抽出
  共享的恢复状态载体）
- `ClaudeRuntimePorts`（14 处）—— 它由 `bindings.ClaudeFactory` 在运行时调用，
  惰性是真的；要去掉只能把工厂改成"组合期先建 ports、调用时只补 cfg"
- 其余 20 处散在 16 个函数里，多为 1-3 处

`BuildUpgrades ← WorkspaceConfiguration` 已经不再是惰性读取：workspace 两个
builder 改成构造期参数后，这条边变成了正向的显式依赖。

### 棘轮

`internal/architecture` 里有两条预算测试，**都只许降**，且降了必须同步改预算：

| 测试 | 起始值 | 当前 | 目标 |
|---|---|---|---|
| `TestFeishuAppAggregateDoesNotGrow` | 541 | 541 | 0 |
| `TestFeishuAppLazyBindingReadsDoesNotGrow` | 86 | **38** | **0** |

### 分析口径的第三次修正：语句级图必须闭包到 wrapper 的实现

2026-10-04 发现：`depmap --bindings` 原来只看赋值语句里那个函数**自己**读了什么。
`BuildWorkspaceConfiguration` 自己什么都不读——真正的读取在
`buildWorkspaceConfigService` 里——于是 `WorkspaceConfiguration` 这条语句在图上
是"零 eager 依赖"，被 `ebabde9` 排到了 `BackendConfiguration` 前面。而
`buildWorkspaceConfigService` 构造期读 `a.bindings.BackendConfiguration`，读到的是
**零值**，绑进 workspace 服务的三个 notice + usage 全是 nil Driver 的方法值，
生产环境第一次工作区切换就会 panic。fixture 的顺序恰好相反，所以测试全绿。

这跟前面三次错误同源：**判据不完整**（先是按名字筛工厂、再是漏掉 `Forward*`、
这次是不闭包调用）。工具已修：`reach()` 会沿 `*App` 调用边和"工厂交出去的结构体的
方法"求传递闭包，并区分 eager/lazy；结构体方法算运行时读取（交出去是依赖，不是
构造顺序约束）。同时新增一节报告**反向 eager 读取**，在修复前的树上是：

```
WorkspaceConfiguration (line 142) reads BackendConfiguration (assigned line 189)
```

修复后为 0。这个检查应当成为每次重排 composition 的前置条件。

顺带发现的死代码：`workspacecmd.BackendWorkspacePermissionCommand` 这条 port 从
`33c4d06` 起就没有调用方（`/workspace permissions` 一直走
`CommandWorkspace → Deps.PermissionDriver()`），它才是把 `BackendConfiguration`
拉进 workspace 服务的原因。已删除；workspace 服务改成从 driver 直接取四个
notice/usage。

## 施工顺序

1. **拓扑排序**（不改语义，纯重排 composition 的构造顺序）—— 消掉所有非环的向后读。
   做完后大部分工厂可以直接套 `BackendRuntimeDeps`。

   注意：**不要靠"交换两行"来做**。写作本文时曾打算交换 `StartupRecovery`(150) 与
   `MaintenanceCommands`(151)，复核发现两者互相依赖（环 2 的一部分），交换只会
   把顺序问题变成环。`Inbound`(202) 与 `ForwardInputs`(203) 同理（环 3）。
   重排前必须按语句级口径重算一遍环。
2. **拆环 3** —— 只有两条边、两个服务，先拿它练手并确认拆法。
3. **拆环 2** —— 五个服务、七条边，`recoverFrontendRuntimeState` 已是一行转发。
4. **拆环 1** —— 涉及核心状态机，需要对照协议审计文档，单独一轮。

## 验证

每一步之后：

- `go build ./...`、`go vet ./...`、`staticcheck ./...`（必须 0）
- `go test -race -count=1 -shuffle=on ./...`
- 环的判定可复现：用 `scripts/depmap` 输出 binding 级依赖图，求强连通分量，
  应当只剩空集

## 不要用惰性 provider 绕

把某条边改成 `func() X` 可以让编译通过（端口本来就是闭包），但那是把环藏起来：
provider 会捕获 `bindings` 或 `App`，正好把要消除的聚合请回来。唯一例外是环 2 里
`CodexUpgrade` 的自引用 —— 自引用没有"更底层的组件"可抽，后置绑定是正当解法。
