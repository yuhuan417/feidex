# `internal/app` 重构执行计划

更新时间: 2026-04-28

相关约束文档:

- [DEVELOPER.md](/home/yuhuan/feidex/DEVELOPER.md)
- [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)
- [docs/app-package-boundaries.md](/home/yuhuan/feidex/docs/app-package-boundaries.md)
- [docs/backend-layering.md](/home/yuhuan/feidex/docs/backend-layering.md)

这份文档是执行计划，不是架构讨论。目标是把 `internal/app` 从“过渡态的大包 + 兼容层”收敛成稳定的组合根和协议编排层，同时不改变现有产品行为。

## 1. 目标

本次重构必须同时满足以下 5 个目标:

1. `internal/app` 根包只保留组合根、事件入口、协议敏感编排和少量 glue code。
2. backend 差异，尤其是 permission / workspace / conversation 术语差异，收敛到明确的 backend driver / capability 边界内。
3. 命令、菜单、卡片 action 共享同一份能力注册表，结构上保证“菜单能力必有 direct command entrypoint”。
4. 删除当前大部分兼容层文件，尤其是 `*_alias.go`、纯转发 `*_adapters.go`、lowercase wrapper facade。
5. 收紧 stringly-typed 边界，避免 `map[string]any` 和裸字符串状态继续在多个包之间传递。

## 2. 非目标

以下内容不在本次重构范围内:

- 不新增用户可见能力。
- 不修改 backend 切换策略、frontend 隔离规则、审批语义。
- 不调整持久化数据结构和磁盘格式，除非单独立项做迁移。
- 不把 live integration tests 并入默认 `go test ./...`。
- 不把 Claude 和 Codex 强行抽象成“完全相同的协议层”；只统一产品边界，不抹平真实差异。

## 3. 执行约束

每个阶段都必须遵守下面的约束。

### 3.1 协议约束

- 只要改动触及 `internal/app` 的 turn/thread lifecycle、approval、tool input、review、compaction、server request，就必须逐项对照 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)。
- 如果某一步确实需要改协议行为，必须在同一个 PR 里同时更新审计文档和回归测试；否则该 PR 不允许合并。

### 3.2 提交约束

- 严格按阶段拆 PR，不允许一个 PR 同时做“状态仓储抽离 + backend driver 合并 + 菜单注册表重写”。
- 每个阶段必须能单独合并、单独回滚。
- 每个阶段结束后，`package app` 根包文件数必须单调下降；不要求一步到位，但不允许新增新的过渡层文件族。

### 3.3 验证约束

每个阶段结束后至少运行:

```bash
./scripts/with_tmp_go_cache.sh go test ./...
```

如果阶段触及协议关键路径，额外运行:

```bash
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*CriticalPath.*|.*StateMachine.*|.*MenuCommandDirectAccess.*|.*BackendSelection.*|.*SubmissionQueueClaude.*|.*CodexTurnRecovery.*)'
```

如果阶段触及 `internal/codexrpc` 或真实 app-server 交互边界，再按 `DEVELOPER.md` 手工运行对应 integration tests；不要把 integration tests 放进默认验证命令。

## 4. 当前问题清单

当前代码里的主要结构问题和对应热点文件如下。

### 4.1 根包仍然过大

- `internal/app` 根层当前仍有大量 `package app` 文件，业务编排、兼容 wrapper、adapter glue、菜单渲染、命令分发混在一起。
- 代表文件:
  - [internal/app/app.go](/home/yuhuan/feidex/internal/app/app.go)
  - [internal/app/feishu_event_router.go](/home/yuhuan/feidex/internal/app/feishu_event_router.go)
  - [internal/app/action_registry_menu.go](/home/yuhuan/feidex/internal/app/action_registry_menu.go)
  - [internal/app/command_registry.go](/home/yuhuan/feidex/internal/app/command_registry.go)
  - [internal/app/menu_registry.go](/home/yuhuan/feidex/internal/app/menu_registry.go)

### 4.2 依赖注入风格混乱

- 一部分包已经是窄接口 + service；另一部分仍是大号 callback bag。
- 代表文件:
  - [internal/app/submission/queue.go](/home/yuhuan/feidex/internal/app/submission/queue.go)
  - [internal/app/turnlifecycle/service.go](/home/yuhuan/feidex/internal/app/turnlifecycle/service.go)
  - [internal/app/workspacecmd/service.go](/home/yuhuan/feidex/internal/app/workspacecmd/service.go)
  - [internal/app/clauderuntime/service.go](/home/yuhuan/feidex/internal/app/clauderuntime/service.go)
  - [internal/app/backend/configuration.go](/home/yuhuan/feidex/internal/app/backend/configuration.go)

### 4.3 backend 差异分散

- permission、workspace config、会话术语等差异没有收敛到单一 backend driver。
- 代表文件:
  - [internal/app/backend/driver.go](/home/yuhuan/feidex/internal/app/backend/driver.go)
  - [internal/app/backend/permission_driver.go](/home/yuhuan/feidex/internal/app/backend/permission_driver.go)
  - [internal/app/backend/actions.go](/home/yuhuan/feidex/internal/app/backend/actions.go)
  - [internal/app/convbackend/service.go](/home/yuhuan/feidex/internal/app/convbackend/service.go)
  - [internal/app/backend/configuration.go](/home/yuhuan/feidex/internal/app/backend/configuration.go)

### 4.4 注册表不是单一事实源

- slash command、本地菜单、卡片 action 分别定义，靠人工保持一致。
- 代表文件:
  - [internal/app/command_registry.go](/home/yuhuan/feidex/internal/app/command_registry.go)
  - [internal/app/menu_registry.go](/home/yuhuan/feidex/internal/app/menu_registry.go)
  - [internal/app/action_registry_menu.go](/home/yuhuan/feidex/internal/app/action_registry_menu.go)

### 4.5 兼容层太多

- 当前存在大量 alias / adapter / wrapper 文件，说明抽离还没完成。
- 代表文件:
  - [internal/app/submission_adapters.go](/home/yuhuan/feidex/internal/app/submission_adapters.go)
  - [internal/app/workspacecmd_adapters.go](/home/yuhuan/feidex/internal/app/workspacecmd_adapters.go)
  - [internal/app/turnlifecycle_adapters.go](/home/yuhuan/feidex/internal/app/turnlifecycle_adapters.go)
  - [internal/app/backend_alias.go](/home/yuhuan/feidex/internal/app/backend_alias.go)
  - [internal/app/runtime_types_alias.go](/home/yuhuan/feidex/internal/app/runtime_types_alias.go)

## 5. 分阶段执行顺序

必须按下面顺序执行。不要跳阶段。

---

## 阶段 0: 建立重构护栏

### 目标

先把现有行为锁住，避免后续每一步都在“边改边猜有没有回归”。

### 必做动作

1. 整理并固定当前必须保绿的测试集，至少覆盖:
   - 命令/菜单直达性: `menu_command_direct_access_test.go`
   - backend 可见性/切换: `backend_selection_test.go`
   - turn/thread 主路径: `critical_paths_test.go`, `critical_paths_more_test.go`
   - state-machine 契约: `state_machine_contracts_test.go`
   - approval / server request 契约: `item_started_server_request_test.go`, `server_request_reply_error_test.go`
   - Claude 队列与会话恢复: `submission_queue_claude_test.go`
   - Codex 恢复: `codex_turn_recovery_test.go`
2. 新增一个“架构约束检查表”到 PR 描述模板或团队约定中，内容至少包括:
   - 是否新增了 `package app` 根层业务逻辑
   - 是否新增了 `*_alias.go`
   - 是否新增了 raw `map[string]any` 跨包传递
   - 是否新增了 `switch configuredBackend(...)` 到 root `app`
3. 在 [docs/app-package-boundaries.md](/home/yuhuan/feidex/docs/app-package-boundaries.md) 和 [docs/backend-layering.md](/home/yuhuan/feidex/docs/backend-layering.md) 增加“本计划执行中，禁止继续扩张兼容层”的说明。

### 必须清零的临时行为

- 从阶段 0 开始，不允许新增新的 root-level `*_alias.go` 文件。
- 从阶段 0 开始，不允许新增新的 root-level `*_adapters.go` 文件，除非是为了替换更大的旧适配器文件，并且在同一 PR 删除旧文件。

### 验收标准

- 基线测试全部通过。
- 后续每个阶段都能复用同一套回归用例。
- 团队对“什么叫结构回退”有统一判定口径。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*CriticalPath.*|.*StateMachine.*|.*MenuCommandDirectAccess.*|.*BackendSelection.*|.*SubmissionQueueClaude.*|.*CodexTurnRecovery.*)'
```

---

## 阶段 1: 抽离 `appstate`，收掉 `appStateFacade`

### 目标

把持久化状态访问从 root `app` 包剥离出来，避免所有 service 都通过 `appState(a)` 间接读写整个 store。

### 必做动作

1. 新建 `internal/app/appstate` 包。
2. 把以下文件迁移到新包，按职责拆成 repo 或 store facade:
   - `internal/app/app_state.go`
   - `internal/app/app_state_session.go`
   - `internal/app/app_state_submission.go`
   - `internal/app/app_state_pending.go`
   - `internal/app/app_state_message.go`
3. 在新包里定义明确的状态边界，至少拆成:
   - `SessionRepo`
   - `SubmissionRepo`
   - `PendingRepo`
   - `MessageLinkRepo`
   - `FrontendNotificationRepo`
4. `App` 不再暴露多义 `appState(a)`；改成显式 getter，例如:
   - `SessionRepo()`
   - `SubmissionRepo()`
   - `PendingRepo()`
   - 或一个明确命名的 `Repos()` / `AppState()`
5. root `app` 包里删除重复包装方法族，例如:
   - `Session` / `GetSession`
   - `Sessions` / `AllSessions`
   - 其他仅为兼容接口命名差异而存在的重复转发
6. `internal/state` 的存储格式不变；本阶段只改变 app 层访问方式，不做数据迁移。

### 需要一起改的调用方

- 所有 `appState(a)` 调用点
- 所有依赖 `appStateFacade` 满足接口的 adapter 文件
- 所有直接在 root `app` 层调用 `store.UpdateSession` / `store.UpsertSession` 的路径

### 必须清零的搜索项

以下搜索结果在阶段结束时必须为 0:

```bash
rg -n 'appState\\(' internal/app
rg -n 'type appStateFacade' internal/app
```

### 验收标准

- root `app` 包不再定义 `appStateFacade`。
- 所有状态写入都通过 `internal/app/appstate` 完成。
- service 依赖的是明确 repo，而不是“整个 store 的万能代理”。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*CriticalPath.*|.*StateMachine.*|.*BackendSelection.*|.*SubmissionQueueClaude.*)'
```

---

## 阶段 2: 统一依赖注入风格，去掉 callback bag

### 目标

所有已抽离的 `internal/app/*` 包，统一成“窄接口 / grouped ports / 明确 constructor”的依赖注入方式，不再继续使用大量裸函数字段。

### 必做动作

1. 统一约定 constructor 形状。允许两种合法形状:
   - `NewService(app App)`，其中 `App` 是窄接口
   - `NewService(deps Deps)`，其中 `Deps` 内部再按领域分组端口
2. 不再允许以下模式继续扩张:
   - exported service struct 上直接挂十几个到几十个 `func(...)` 字段
   - 同一个包同时存在 `App interface` 和大号 callback bag 两套注入方式
3. 依次重构以下包:
   - `internal/app/workspacecmd`
   - `internal/app/backend`
   - `internal/app/clauderuntime`
4. 对 `workspacecmd` 的具体要求:
   - 把 [internal/app/workspacecmd/service.go](/home/yuhuan/feidex/internal/app/workspacecmd/service.go) 里的 callback fields 收敛成 `Deps` 结构。
   - `ConfigService`、`ManagementService`、`RenderService`、`ThreadService` 只依赖分组端口，不再直接持有数十个函数。
5. 对 `clauderuntime` 的具体要求:
   - 把 [internal/app/clauderuntime/service.go](/home/yuhuan/feidex/internal/app/clauderuntime/service.go) 里的 callback fields 收敛成接口，例如:
     - `LifecycleSink`
     - `TurnStreamSink`
     - `InteractiveRequestSink`
     - `SessionLookup`
     - `PermissionConfig`
   - `clauderuntime.Service` 不再直接暴露大量外部注入回调字段。
6. 对 `backend` 的具体要求:
   - 把 [internal/app/backend/configuration.go](/home/yuhuan/feidex/internal/app/backend/configuration.go) 当前的 function fields 收敛成分组端口。
7. 删除明显的占位 adapter 和死代码，尤其是 [internal/app/submission_adapters.go](/home/yuhuan/feidex/internal/app/submission_adapters.go) 里目前不应继续保留的 placeholder adapter。

### 必须清零的搜索项

以下文件在阶段结束时不得再包含“仅为了兼容而保留的大号 callback bag”:

- `internal/app/workspacecmd/service.go`
- `internal/app/clauderuntime/service.go`
- `internal/app/backend/configuration.go`

以下占位代码必须删除:

```bash
rg -n 'Placeholder|placeholder|actual implementation is in the wrapper|return nil, nil|return \"\"' internal/app/submission_adapters.go internal/app/*adapters.go
```

### 验收标准

- `workspacecmd`、`backend`、`clauderuntime` 都只保留一种注入模式。
- 不再存在“导出 service + 大量裸函数字段 + root 包再赋值”的模式。
- 删除所有明显占位 adapter。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*Workspace.*|.*BackendSelection.*|.*Claude.*|.*CriticalPath.*)'
```

---

## 阶段 3: 收根包，让 `internal/app` 成为真正的组合根

### 目标

root `app` 包只保留:

- `App` / `Service` 构造与生命周期
- Feishu 事件入口
- 顶层路由
- 状态机敏感编排
- 少量跨包 glue

其他业务逻辑必须下沉到 owning package。

### 必做动作

1. 逐个审计 root `package app` 文件，并为每个文件标记归属:
   - `bootstrap`
   - `routing`
   - `protocol-sensitive orchestration`
   - `feature logic`
   - `compat shim`
2. 迁移 `feature logic` 到 owning package；优先处理以下文件族:
   - `history*.go`
   - `workspace_*`
   - `upgrade*.go`
   - `review*.go`
   - `skills*.go`
   - `model_config*.go`
   - 其他纯渲染 / 纯配置分发逻辑
3. 删除兼容 facade 文件，把 backend / conversation 的职责真正下沉到 `internal/app/backend` 和 `internal/app/convbackend`:
   - `internal/app/backend/driver.go`
   - `internal/app/backend/permission_driver.go`
   - `internal/app/backend/actions.go`
   - `internal/app/convbackend/service.go`
4. 删除无必要 alias 文件族；保留 alias 的唯一允许条件是“正在同一阶段内迁移调用方，且下一阶段删除”。
5. root `app` 包里禁止继续新增“package-level helper + *App 第一参数”的业务函数。新业务必须先判断是否应该属于子包。

### 必须清零的搜索项

以下文件族要么删除，要么只剩极薄的组合根 glue:

```bash
find internal/app -maxdepth 1 -name '*alias*.go' -o -name '*_alias.go'
find internal/app -maxdepth 1 -name '*adapters.go'
```

以下 root backend glue 文件在阶段结束时不得继续承担真正业务决策:

- `internal/app/backend_runtime.go`
- `internal/app/backend_actions.go`
- `internal/app/backend_configuration_helpers.go`

### 验收标准

- root `app` 包只保留组合根和协议敏感编排。
- 兼容 facade / alias 文件数量明显下降，并且不再新增。
- 业务逻辑从 root 包迁走后，测试仍保持不变。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*History.*|.*Review.*|.*Upgrade.*|.*Skills.*|.*CriticalPath.*)'
```

---

## 阶段 4: 建立 backend driver，收敛 permission 差异

### 目标

把 Codex / Claude 的产品级差异收敛到单一 backend driver 边界内，尤其是 permission 差异。

### 必做动作

1. 在 `internal/app/backend` 内建立统一 driver 体系，至少包括:
   - `Driver`
   - `RuntimeDriver`
   - `ConversationDriver`
   - `PermissionDriver`
   - `CapabilitySet`
2. `PermissionDriver` 必须明确表达每个 backend 支持的配置作用域:
   - global
   - workspace
   - session / thread
3. `PermissionDriver` 必须成为以下行为的唯一 owner:
   - permission 命令词汇
   - permission 菜单与卡片文案
   - permission 解析与应用
   - 当前 session / workspace 的 permission 摘要展示
4. 把散落在 root `app` 或多个子包里的 backend `switch` 合并到 driver 内部，重点改动文件:
   - `internal/app/backend/configuration.go`
   - `internal/app/threadmenu/service.go`
   - `internal/app/workspacecmd/*`
   - `internal/app/modelconfig/*`
   - `internal/app/backend/driver.go`
   - `internal/app/convbackend/service.go`
5. 定义一条明确规则:
   - root `app` 包不再直接判断“Claude 用 permissions / Codex 用 sandbox + policy”这类产品语义。
   - root 只选择 backend driver；具体语义由 driver 返回。

### 必须清零的搜索项

除 `internal/app/backend` 包及其测试外，以下搜索结果必须清零:

```bash
rg -n 'switch (appcore\\.)?ConfiguredBackend|switch configuredBackend|case appruntime\\.BackendClaude|case appruntime\\.BackendCodex|case backendClaude|case backendCodex' internal/app
```

允许保留的例外:

- 组合根里做一次 driver 选择
- backend 包内部实现
- 测试代码

### 验收标准

- permission 差异只在 backend driver 里定义一次。
- workspace / session / thread 相关 UI 和命令不再分散 hardcode。
- 新 backend 差异可以通过 driver 扩展，而不是继续往 root 包里加 `switch`。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*BackendSelection.*|.*MenuCommandDirectAccess.*|.*Model.*|.*Workspace.*|.*ClaudePermission.*)'
```

---

## 阶段 5: 合并命令 / 菜单 / action 注册表

### 目标

建立一个单一的用户能力注册表，让 slash command、菜单节点、帮助文案、卡片 action 同源。

### 必做动作

1. 新建一个专门的 feature / capability registry 包。推荐名称:
   - `internal/app/features`
   - 如果不新增包，则扩展 `internal/app/menutypes`
2. 定义统一注册项，至少包含:
   - `ID`
   - `Backends`
   - `SlashCommands`
   - `HelpEntries`
   - `MenuNode`
   - `ActionNames`
   - `Renderer`
   - `Handler`
3. 用这份注册表生成或派生以下现有结构:
   - [internal/app/command_registry.go](/home/yuhuan/feidex/internal/app/command_registry.go)
   - [internal/app/menu_registry.go](/home/yuhuan/feidex/internal/app/menu_registry.go)
   - [internal/app/action_registry_menu.go](/home/yuhuan/feidex/internal/app/action_registry_menu.go)
4. 对帮助文案、菜单按钮、action route 的 backend 可见性，一律从同一份注册表派生；不再各自 hardcode。
5. 增加结构性测试:
   - 每个菜单能力必须至少声明一个 direct command entrypoint
   - 每个 action name 只能归属一个 feature
   - backend 隐藏能力不会出现在 `/help` 和菜单中

### 必须清零的搜索项

以下三个注册入口不能继续独立演化:

```bash
rg -n 'localCommandSpecList|menuNodeRenderers|menuCardActionHandlers' internal/app
```

阶段结束时，上述符号要么删除，要么只作为从统一注册表派生出的只读结构存在，且禁止手工维护。

### 验收标准

- 用户能力只有一份声明源。
- “菜单能力必须有 direct command entrypoint” 由结构保证，不再靠人工检查。
- backend filter 逻辑只声明一次。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*MenuCommandDirectAccess.*|.*Commands.*|.*BackendSelection.*|.*Help.*)'
```

---

## 阶段 6: 收紧字符串状态和 raw payload 边界

### 目标

把“协议层原始数据”和“app 语义层内部状态”分开，减少后续回归风险。

### 必做动作

1. 为以下状态定义 typed constant；保持序列化值不变:
   - session status
   - submission status
   - pending request status
   - approval kind
   - menu action name
2. 替换 root `app` 和子包里的裸字符串状态写入。
3. 协议边界要求:
   - Codex raw notification / server request 只允许在 `internal/app/backend` 的边界文件里处理原始 `json.RawMessage` 和 `map[string]any`
   - Claude raw event 到 app 语义层之前，也必须收敛成 typed DTO
4. 对 turn item、approval payload、tool user input payload，新增明确 DTO；不要继续跨多个包传递 raw `map[string]any`。
5. `turnitem` 和 `turnstream` 的输入边界要从“原始 item map”收敛到“小型 typed payload + 必要原始扩展字段”的组合。

### 必须清零的搜索项

阶段结束时，以下问题必须显著收敛:

```bash
rg -n 'Status = "' internal/app
rg -n 'map\\[string\\]any' internal/app
```

允许保留的例外:

- 协议 transport 边界
- Feishu card 原始 JSON 组装边界
- 测试构造数据

### 验收标准

- 业务状态不再靠散落字符串字面量维护。
- raw payload 不再跨越多个包传递。
- 语义层测试可以直接基于 typed DTO 编写。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*StateMachine.*|.*CriticalPath.*|.*Approval.*|.*Pending.*|.*Turn.*)'
```

---

## 阶段 7: 删除剩余兼容层并更新边界文档

### 目标

完成最后清理，把过渡态痕迹删掉，更新文档，使仓库进入稳定结构。

### 必做动作

1. 删除不再需要的 alias / wrapper / adapter 文件。
2. 更新以下文档，使其反映最终结构:
   - [docs/app-package-boundaries.md](/home/yuhuan/feidex/docs/app-package-boundaries.md)
   - [docs/backend-layering.md](/home/yuhuan/feidex/docs/backend-layering.md)
   - 如果用户可见能力名称或可见性发生变化，再更新 [docs/capabilities.md](/home/yuhuan/feidex/docs/capabilities.md)
3. 在根包增加一条明确规则:
   - root `app` 包不再接受新的兼容 shim
   - 新业务默认先找 owning package，再考虑是否真的必须进 root
4. 对剩余 root `package app` 文件做一次最终审计，确认每个文件都属于:
   - bootstrap
   - routing
   - protocol-sensitive orchestration
   - minimal glue

### 必须清零的搜索项

```bash
find internal/app -maxdepth 1 -name '*alias*.go' -o -name '*_alias.go'
```

如果还有残留文件，必须在文档里逐个解释保留原因和删除时间点；不允许“先留着以后再说”。

### 验收标准

- 兼容层文件族基本清空。
- 文档和最终代码边界一致。
- 团队以后新增功能时，不会再默认把逻辑塞回 root `app`。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
```

---

## 阶段 8: 继续瘦根包，收回重复 orchestration

### 目标

在不改协议状态机语义的前提下，继续把已经有 owner package 的逻辑从 root `app` 包收回去，优先删除“root 重复实现”和“root backend helper”。

### 本阶段范围

本阶段只做下面 4 类收敛，不额外开新 feature:

1. `pending queue` 的实际实现统一收敛到 [internal/app/submission](/home/yuhuan/feidex/internal/app/submission)，root `app` 只保留薄包装。
2. `conversation backend` 的 resume / interrupt / steer / startup-recovery helper 收敛到 [internal/app/convbackend](/home/yuhuan/feidex/internal/app/convbackend)。
3. `local upgrade` 的 picker / staging / confirm 流程收敛到 [internal/app/upgradecmd](/home/yuhuan/feidex/internal/app/upgradecmd)。
4. 同步更新边界文档，明确这几块 owner 已经迁移完成。

### 必做动作

1. 删除 root `app` 中和 [internal/app/submission/pending_queue.go](/home/yuhuan/feidex/internal/app/submission/pending_queue.go) 重复的 pending queue 业务实现。
   - [internal/app/pending_inputs.go](/home/yuhuan/feidex/internal/app/pending_inputs.go) 只允许保留 root 兼容包装和少量测试兼容符号。
   - 真实逻辑必须由 `submission.PendingQueueService` 提供。
2. 把以下 helper 从 root `app` 迁到 [internal/app/convbackend](/home/yuhuan/feidex/internal/app/convbackend):
   - [internal/app/conversation_backend_helpers.go](/home/yuhuan/feidex/internal/app/conversation_backend_helpers.go)
   - [internal/app/conversation_backend_startup.go](/home/yuhuan/feidex/internal/app/conversation_backend_startup.go)
3. 把以下本地升级流程迁到 [internal/app/upgradecmd](/home/yuhuan/feidex/internal/app/upgradecmd):
   - `commandUpgradeLocalPick`
   - `commandUpgradeLocalPath`
   - `createUpgradeLocalPickerRequest`
   - `createLocalUpgradeRequest`
   - `completeUpgradeLocalPick`
   - `completeUpgradeLocalBinaryConfirm`
   - `stageLocalUpgradeArtifact`
4. 更新 [docs/app-package-boundaries.md](/home/yuhuan/feidex/docs/app-package-boundaries.md)，把：
   - `submission` 标记为 pending queue / reaction owner
   - `convbackend` 标记为 conversation resume / startup recovery owner
   - `upgradecmd` 标记为 local upgrade orchestration owner

### 协议核对要求

本阶段触及的协议敏感区域包括:

- `SM-04` `TurnLifecycleCore`
- `SM-05` `TurnSteerContinuation`
- `SM-09` `CommandApproval`
- `SM-10` `FileApproval`
- `SM-11` `ToolRequestUserInput`
- `SM-22` `PermissionsApproval`
- `SM-23` `McpElicitationRequest`

要求:

- 不改变 pending request 的创建、reply、resolved、resume 时序。
- 不改变 Codex `turn/steer`、`thread/resume`、`thread/start` 的请求形状。
- 不改变 Claude session resume / interrupt / user-input resolve 的外部行为。

### 必须清零的搜索项

阶段结束时，下面两个 root 重复实现文件必须删除或降为纯薄包装:

```bash
rg -n 'func \\(s pendingQueueService\\) ' internal/app/pending_inputs.go
rg -n 'func (resumeClaudeSelectedThread|resumeCodexSelectedThread|interruptCodexActiveTurn|continueCodexActiveTurn|tryCodexReplyContinuation|recoverClaudeStartupConversation|recoverCodexStartupConversation)\\(' internal/app
```

### 验收标准

- root `app` 不再保留 pending queue 的完整业务实现。
- `conversation backend` helper / startup recovery 不再以 root 文件族存在。
- `local upgrade` 的主要业务流进入 `upgradecmd`，root 仅保留 wrapper。
- `go test ./...` 通过，且关键状态机回归测试保持通过。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*CriticalPath.*|.*StateMachine.*|.*Pending.*|.*Approval.*|.*SubmissionQueueClaude.*|.*CodexTurnRecovery.*|.*Upgrade.*)'
```

---

## 阶段 9: 建立 `serverrequest` owner package，统一 pending request 流程

### 目标

把当前分散在 root `app` 包里的 pending request 相关业务流收敛成一个明确 owner。阶段结束后，root `app` 只保留:

- action registry / routing 入口
- 极薄的 `*_bindings.go`
- 必须直接接触 `*App` 字段的 glue

pending request 的创建、渲染、reply、cancel、resolved 后恢复，必须由单一 package 管理。

### 本阶段范围

本阶段只处理以下 4 类请求流:

1. tool user input
2. MCP elicitation form / URL
3. approval（command / file / permissions）
4. backend-specific pending request reply adapter

本阶段不处理:

- review 表单
- workspace new / clone 表单
- path picker 主流程
- final card patch 或 turn stream

### 推荐 owner 包

新建:

- `internal/app/serverrequest`

该包的职责边界:

- pending request payload 结构和解析
- approval / user-input / elicitation card 构造
- reply / cancel / quick answer / form answer / URL decision
- backend-specific server-request reply adapter
- resolved 之后是否恢复 submission 的纯决策或 service-level orchestration

该包不负责:

- Feishu 事件路由
- root `App` 生命周期其它逻辑
- review/workspace 特定表单
- `turn/completed` 收口

### 必做动作

1. 把以下 root 文件的主要业务逻辑迁入 `serverrequest`:
   - [internal/app/pending_forms.go](/home/yuhuan/feidex/internal/app/pending_forms.go)
   - [internal/app/tool_user_input_forms.go](/home/yuhuan/feidex/internal/app/tool_user_input_forms.go)
   - [internal/app/user_input_actions.go](/home/yuhuan/feidex/internal/app/user_input_actions.go)
   - [internal/app/elicitation_forms.go](/home/yuhuan/feidex/internal/app/elicitation_forms.go)
   - [internal/app/approval_actions.go](/home/yuhuan/feidex/internal/app/approval_actions.go)
   - [internal/app/approval_cards.go](/home/yuhuan/feidex/internal/app/approval_cards.go)
   - [internal/app/backend_server_request_adapter.go](/home/yuhuan/feidex/internal/app/backend_server_request_adapter.go)
2. 在 `serverrequest` 包内明确分文件，不允许再形成一个新的 god file。最低建议拆成:
   - `types.go`: kind / payload / typed DTO
   - `adapter.go`: Codex / Claude reply adapter
   - `approval.go`: approval action + resolved card
   - `user_input.go`: quick answer / form answer / text answer
   - `elicitation.go`: form / URL request and reply
   - `cancel.go`: cancel flow
   - `service.go` 或 `deps.go`: 注入边界
3. `tool user input` 和 `elicitation` 的 payload 类型继续复用或下沉现有:
   - [internal/app/pendingforms](/home/yuhuan/feidex/internal/app/pendingforms)
4. `approval` 的展示和按钮构造继续复用现有:
   - [internal/app/approval](/home/yuhuan/feidex/internal/app/approval)
   - [internal/app/approvalview](/home/yuhuan/feidex/internal/app/approvalview)
5. root `app` 允许保留的内容仅限:
   - registry 把 action name 映射到 `serverrequest.Service`
   - 少量 test-only compatibility shim
6. 如果阶段过程中发现 review / workspace form 和 `serverrequest` 逻辑强耦合，不允许顺手一起并入；必须留到后续阶段单独处理。

### 协议核对要求

本阶段是高风险协议阶段，必须逐项核对:

- `SM-09` `CommandApproval`
- `SM-10` `FileApproval`
- `SM-11` `ToolRequestUserInput`
- `SM-22` `PermissionsApproval`
- `SM-23` `McpElicitationRequest`

必须保持不变的语义:

1. pending request 创建后，只有 reply 成功才允许本地推进到 replied / resolved 后续状态。
2. `serverRequest/resolved` 仍然是恢复 submission 的唯一 resolved 边界。
3. user input / elicitation 的 cancel 路径不能跳过协议 reply。
4. permissions approval 不能退回裸 map string switch 的实现方式。
5. Claude / Codex 的 reply payload shape 不能被“统一抽象”错误抹平。

### 必须清零的搜索项

阶段结束时，以下 root 文件必须删除或降为纯 glue:

```bash
rg -n 'func (completeUserInputAnswer|completeUserInputQuickAnswer|completeUserInputFormSubmit|completeUserInputMultiToggle)\\(' internal/app
rg -n 'func (sendUserInputFormCard|sendElicitationFormCard|sendElicitationURLCard|completeElicitationURLAction)\\(' internal/app
rg -n 'func (completeApprovalAction|renderResolvedApprovalCard|resumeSubmissionAfterRequest)\\(' internal/app
rg -n 'type serverRequestBackendAdapter|func serverRequestAdapterForPending\\(' internal/app
```

允许残留的 root 符号:

- action registry 中的路由入口
- test-only `_test.go` 兼容导出

### 验收标准

- pending request 主流程存在单一 owner package。
- root `app` 不再同时持有 approval / user input / elicitation / adapter 四套业务实现。
- 相关状态机测试仍然全部通过。
- 不新增新的 root wrapper 文件族来“过桥”。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*Approval.*|.*Pending.*|.*StateMachine.*|.*ItemStartedServerRequest.*|.*ServerRequestReplyError.*)'
```

---

## 阶段 10: 收走 Claude submission 主流程，压缩 root `submission_*` orchestration

### 目标

把剩余的 submission 主流程进一步从 root `app` 收回到 owner package，尤其是 Claude-specific turn start / retry / rollback / bind turn。阶段结束后，root `app` 不应继续保留大块 submission 启动链路实现。

### 本阶段范围

重点处理以下文件:

- [internal/app/submission_queue_claude.go](/home/yuhuan/feidex/internal/app/submission_queue_claude.go)
- [internal/app/submission_queue.go](/home/yuhuan/feidex/internal/app/submission_queue.go)
- [internal/app/submission_status.go](/home/yuhuan/feidex/internal/app/submission_status.go)
- [internal/app/replycontinuation_bindings.go](/home/yuhuan/feidex/internal/app/replycontinuation_bindings.go)
- [internal/app/convbackend_bindings.go](/home/yuhuan/feidex/internal/app/convbackend_bindings.go) 中与 `StartNextSubmission` 强绑定的薄 glue

本阶段不处理:

- turn lifecycle service 本体
- turn stream service 本体
- backend runtime recovery 本体

### 推荐 owner 方向

优先方案:

- 扩展 [internal/app/submission](/home/yuhuan/feidex/internal/app/submission) 成为完整 submission orchestration owner

允许的次优方案:

- 如果 Claude-specific 启动逻辑确实太重，可在 `submission` 包内再拆 `claude_start.go`、`codex_start.go`、`failure.go`、`status.go`

不允许的方案:

- 把同一套逻辑一半留 root、一半复制到 `submission`
- 新增 `submission_helpers_root.go` 这类过渡层

### 必做动作

1. 把 Claude-specific 启动逻辑下沉:
   - fresh session start
   - resumed session failure -> fresh retry
   - steer submission start
   - rollback stale start state
   - bind submission start state
2. 把 submission 状态辅助逻辑下沉:
   - by turn 查 submission
   - pending status refresh
   - source message / root binding bookkeeping 中和 submission 本身强相关的部分
3. 让 [internal/app/convbackend](/home/yuhuan/feidex/internal/app/convbackend) 不再直接依赖 root 里的大号 submission coordinator 逻辑。
4. `replycontinuation` 对 Claude continuation submission 的启动，改为依赖 `submission` owner，而不是 root 私有实现。
5. root `submission_queue.go` 只保留:
   - compatibility wrapper
   - 少量 protocol-sensitive bridge
   - 不能再持有成片业务逻辑

### 协议核对要求

本阶段必须核对:

- `SM-04` `TurnLifecycleCore`
- `SM-05` `TurnSteerContinuation`
- `SM-06` `TurnInterruptCompletion`
- `SM-07` `TurnErrorFailureCompletion`

必须保持不变的语义:

1. `turn/start` 失败后的回滚逻辑不能提前 finalize submission。
2. Claude steer submission 仍然共享当前会话 turn 语义，不能误开独立 CLI session。
3. queued submission 的推进时机仍受 in-flight / live-thread / recovery 状态约束。
4. missing queued submission、runtime unavailable、resume stale session 的恢复路径不能退化。

### 必须清零的搜索项

```bash
rg -n 'type claudeSubmissionService|func newClaudeSubmissionService\\(' internal/app
rg -n 'func \\(w \\*claudeSubmissionService\\) ' internal/app
rg -n 'func \\(w \\*submissionCoordinator\\) (startNextSubmission|startNextSubmissionWithFailureNotice|startNextCodexSubmissionWithFailureNotice|startNextSubmissionAsync)\\(' internal/app
```

允许残留的 root 符号:

- 纯兼容包装
- 明确协议敏感桥接

### 验收标准

- Claude submission start/retry/bind/rollback 有明确子包 owner。
- root `submission_queue*.go` 文件族不再是主要业务实现位置。
- `replycontinuation`、`convbackend`、`turnlifecycle` 与 submission 的依赖边界更清晰。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*SubmissionQueueClaude.*|.*CriticalPath.*|.*CodexTurnRecovery.*|.*SessionStatus.*|.*Reply.*)'
```

---

## 阶段 11: 测试归位与 root `app` 最终审计

### 目标

完成最后一轮结构收口。重点不是再搬业务，而是让测试和代码边界一致，并对 root `package app` 做一次明确、可审计的最终分类。

### 本阶段范围

1. 把 feature-local 测试从 root `app` 迁到 owning package。
2. 精简 root `app` 的测试职责，只保留跨边界集成路径和状态机 guard。
3. 对 root `app` 生产文件做最终审计和留存说明。

### 必做动作

1. 以 feature 为单位，逐步把下列类型测试迁走:
   - approval / pending request 相关测试 -> 对应 owner package
   - submission / pending queue 相关测试 -> `submission`
   - conversation resume / startup recovery 相关测试 -> `convbackend`
   - local upgrade 相关测试 -> `upgradecmd`
2. root `app` 允许保留的测试只限以下类别:
   - Feishu event routing 整体入口
   - command/menu/action registry 直达性
   - 跨 package 的 critical path
   - state-machine 契约 guard
3. 在文档中为 root `app` 剩余生产文件建立最终分类表，至少按这 4 类:
   - bootstrap / composition
   - routing / registries
   - protocol-sensitive orchestration
   - minimal glue
4. 对每个仍然较大的 root 文件写明保留原因；不允许“因为历史原因先放着”这种模糊结论。
5. 如果阶段结束后 root `app` 仍有明显超大文件，必须在文档里列出下一轮单独计划，而不是默认回流到日常开发中继续膨胀。

### 推荐测试迁移顺序

1. `upgrade_*_test.go`
2. `pending_*_test.go` / `approval_*_test.go`
3. `submission_queue_claude_test.go` 与其同主题补充测试
4. `conversation` / `reply continuation` 相关 feature-local 测试

### 必须清零的搜索项

以下搜索结果原则上应显著减少；如仍保留，必须在 PR 描述中逐项解释:

```bash
find internal/app -maxdepth 1 -name '*_test.go'
find internal/app -maxdepth 1 -name '*.go' ! -name '*_test.go'
```

硬性要求不是“清零 root 测试文件”，而是:

- feature-local 测试不应继续默认堆在 root `app`
- root 生产文件必须都有明确 owner 理由

### 验收标准

- 测试布局与 package owner 基本一致。
- root `app` 的测试重心回到集成和协议 guard。
- root `app` 生产文件完成最终审计并写入文档。
- 团队以后新增 feature 时，有明确规则判断测试应放在哪个 package。

### 建议验证命令

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test ./internal/app -run 'Test(.*CriticalPath.*|.*StateMachine.*|.*MenuCommandDirectAccess.*|.*BackendSelection.*)'
```

---

## 6. 推荐 PR 切分

不要按“目录”切 PR，要按“可验证的结构增量”切。

推荐顺序:

1. PR-1: 阶段 0
2. PR-2: 阶段 1
3. PR-3: 阶段 2 的 `workspacecmd`
4. PR-4: 阶段 2 的 `clauderuntime`
5. PR-5: 阶段 2 的 `backend`
6. PR-6: 阶段 3
7. PR-7: 阶段 4
8. PR-8: 阶段 5
9. PR-9: 阶段 6
10. PR-10: 阶段 7
11. PR-11: 阶段 8
12. PR-12: 阶段 9
13. PR-13: 阶段 10
14. PR-14: 阶段 11

规则:

- 每个 PR 只跨一个主主题。
- 每个 PR 必须附带“清零了哪些搜索项、还剩哪些阶段搜索项未处理”。
- 每个 PR 合并前都必须重新跑完整 `go test ./...`。

## 7. 完成判定

只有同时满足下面条件，才算本计划完成:

1. root `app` 包不再承担大面积 feature logic。
2. `appStateFacade` 被 `appstate` 边界替换。
3. backend driver 成为 Codex / Claude 差异的唯一 owner。
4. command / menu / action 共用同一份能力注册表。
5. raw `map[string]any` 只停留在协议或卡片 JSON 边界。
6. alias / adapter / wrapper 兼容层文件族基本删除。
7. 所有相关文档已更新，并与代码边界一致。

## 8. 不允许的捷径

以下做法在执行过程中明确禁止:

- 为了“先跑通”再新增一层 root `app` wrapper。
- 为了“少改调用方”继续保留重复命名接口，例如 `Session` / `GetSession`、`Sessions` / `AllSessions`。
- 为了“减少抽象工作量”把 backend 差异继续写成 scattered `switch backend`。
- 为了“少动测试”跳过 state-machine 契约核对。
- 在一个 PR 里同时引入新 feature 和结构重构。

执行优先级只有一个: 先把边界收正，再谈局部代码美化。
