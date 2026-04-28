# Root `internal/app` 下一轮收敛计划

> Post Phase 11 dedicated follow-up plan

## 目标

这份计划是 Phase 11 之后的**单独执行计划**，不是“以后顺手再改”的备忘。

目标只有两个：

1. 继续压缩 root `internal/app` 的大文件族和 feature glue。
2. 把仍然依赖 root 的窄测试一起迁回 owning package，避免 root 再次长出新的 feature-local unit test。

## 执行规则

- 必须单独立项执行，不和普通 feature PR 混做。
- 每一轮都要同时收 production owner 和对应测试 owner，不能只搬代码不搬测试。
- 触及 lifecycle / approval / server request / review / compaction / tool input 时，必须对照 [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)。

## Wave 1: Workspace / Path Picker 下沉

### 范围

- `workspacecmd_bindings.go`
- `workspacecmd_bindings_config.go`
- `workspacecmd_bindings_management.go`
- `workspacecmd_bindings_render.go`
- `workspacecmd_bindings_thread.go`
- `workspace_creation.go`
- `workspace_creation_clone.go`
- `workspace_creation_render.go`
- `workspace_delete.go`
- `workspace_threads.go`
- `path_picker_actions.go`
- `path_picker_render.go`
- `workspace_feature_actions.go`
- `workspace_menu.go`

### 动作

1. 把 root 中仍然保留的 workspace/path-picker 行为继续压到 `workspacecmd` 和 `pathpick`。
2. root 只保留 feature registration、Feishu 入口 glue、以及必须直接触达 `*App` 的极薄包装。
3. 同步迁出或重写：
   - `path_picker_test.go`
   - `workspace_threads_more_test.go`
   - 与 workspace/path-picker 直接绑定、但不需要完整 root 路由入口的剩余 root 测试

### 退出标准

- root 不再拥有 workspace/path-picker 主业务流。
- 相关测试默认落在 `workspacecmd` / `pathpick` owner。

## Wave 2: Upgrade / Review / History / Debug 收口

### 范围

- `upgrade.go`
- `upgrade_local.go`
- `claude_upgrade*.go`
- `codex_upgrade*.go`
- `review.go`
- `review_bindings.go`
- `review_callback_async.go`
- `review_forms.go`
- `review_git.go`
- `history_bindings.go`
- `debugview_bindings.go`
- `card_demo.go`

### 动作

1. 继续扩展 `upgradecmd`，把 root upgrade glue 压到 owner 包。
2. 为 review/history/debug 建立更清晰的 owner 边界，减少 root 上的 binding 文件。
3. 同步迁出或收窄：
   - `upgrade_test.go`
   - `upgrade_more_test.go`
   - `upgrade_isolation_test.go`
   - `review_test.go`
   - `review_critical_test.go`
   - `history_more_test.go`
   - `card_demo_more_test.go`

### 退出标准

- root 只保留 upgrade/review/history/debug 的注册和入口 glue。
- 纯 helper / 纯 rendering / 纯 form-flow 测试不再默认留在 root。

## Wave 3: Backend Runtime / Startup Recovery / Failure Glue 压缩

### 范围

- `backend_runtime.go`
- `backend_runtime_codex.go`
- `backend_runtime_claude.go`
- `backend_runtime_helpers.go`
- `backend_state_service.go`
- `backend_upgrade_state.go`
- `backend_configuration_helpers.go`
- `backend_actions.go`
- `backend_failure.go`
- `backend_maintenance_scaffold.go`
- `startup_recovery.go`
- `conversation_backend_menu.go`
- `service_tier.go`
- `claude_support.go`

### 动作

1. 把 root 上仍然偏重的 runtime / failure / maintenance glue 压向 `backend`、`convbackend`、`clauderuntime` 等 owner。
2. 保留 root 的前提只限：
   - frontend 级生命周期组合
   - 协议敏感总入口
   - 无法在 owner 包内表示的极薄 cross-owner glue
3. 同步迁出或收窄：
   - `backend_failure_test.go`
   - `backend_core_more_test.go`
   - `backend_selection_test.go` 中 owner-local 的部分
   - `claude_permission_config_test.go`
   - `service_tier_more_test.go`
   - `maintenance_more_test.go`
   - `runtime_maintenance_more_test.go`

### 退出标准

- root 不再拥有成片 runtime/failure helper。
- backend 语义差异继续收敛在 owner 包，而不是重新回流 root。

## Wave 4: Residual Root Test Cleanup

### 目标

在前三个 wave 完成后，对 root `internal/app` 再做一次测试清点，把仍然可迁走的纯 helper / pure rendering / narrow owner-local 测试全部迁走。

### 重点对象

- `pending_inputs_more_test.go`
- `pending_inputs_test.go`
- `helpers_more_test.go`
- `turn_stream_test.go`
- `turn_item_cards_more_test.go`
- `reply_card_split*_test.go`
- `quiet_working_card*_test.go`
- `outbound_cards_more_test.go`
- `local_file_links_more_test.go`
- `token_usage_test.go`

### 退出标准

- root 测试只剩真正的入口、registry、critical path、state-machine guard。
- 不再需要通过文档解释“这些 root-local 测试为什么还暂时留着”。
