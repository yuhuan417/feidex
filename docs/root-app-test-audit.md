# Root `internal/app` 测试现状

更新时间: 2026-07-10

这份文档记录 root `internal/app` 测试的当前保留理由，不再沿用历史 Phase 叙述。目标很简单：说明为什么这些测试还在 root，以及今后什么测试不应该再回到 root。

## 快照

- root `internal/app` 当前保留 `89` 个测试文件，共 `27,405` 行。
- root 测试仍然是“少量必须留在组合根的测试”，而不是默认的 feature-local 单元测试落点。
- owner-local 纯测试已经继续下沉到 `submission`、`serverrequest`、`delivery`、`pathpick`、`workspacecmd`、`servicetiercmd`、`features`、`attachments`、`cards`、`usageview`、`turnitem`、`turn` 等子包。

## root 测试只保留 4 类

### 1. Feishu Routing / Entry-Point Tests

这类测试验证 Feishu 入口、通知路由、异步卡片回调和 frontend-scoped `App` 组合边界。

代表文件:

- `card_action_async_test.go`
- `feishu_notifier_test.go`
- `notifications_test.go`
- `notifications_more_test.go`
- `notifications_branches_more_test.go`
- `frontend_idle_test.go`
- `inbound_deduper_test.go`
- `service_more_test.go`
- `actions_dispatch_more_test.go`

### 2. Registry / Direct-Access Contract Tests

这类测试守卫“菜单能力必须有 direct command entrypoint”和 root registry 的可达性。

代表文件:

- `menu_command_direct_access_test.go`
- `commands_test.go`
- `action_registry_contracts_test.go`
- `feature_registry_test.go`
- `model_config_test.go`
- `model_config_more_test.go`

以下测试导出文件只是为这类 contract 测试提供 owner package 符号，不构成独立保留类别:

- `debugview_test_exports_test.go`
- `history_test_exports_test.go`
- `reviewcmd_test_exports_test.go`
- `thread_test_exports_test.go`
- `upgradecmd_test_exports_test.go`
- `workspacecmd_test_exports_test.go`

### 3. Cross-Package Critical Path Tests

这类测试依赖完整 `App` 组合根、真实 store / session / runtime wiring，验证的是跨 package 的主业务路径，不是 owner-local 单测。

代表文件:

- `app_more_test.go`
- `backend_selection_test.go`
- `review_test.go`
- `review_critical_test.go`
- `upgrade_test.go`
- `upgrade_isolation_test.go`
- `path_picker_test.go`
- `history_more_test.go`
- `claude_core_test.go`
- `skills_test.go`
- `mcp_service_test.go`
- `plan_mode_test.go`

### 4. State-Machine Guard Tests

这类测试直接守卫 turn lifecycle、approval、server request、resume/recovery、retry、interrupt、compact 等协议敏感边界。

代表文件:

- `critical_paths_test.go`
- `critical_paths_more_test.go`
- `state_machine_contracts_test.go`
- `protocol_business_logic_test.go`
- `item_started_server_request_test.go`
- `server_request_reply_error_test.go`
- `codex_turn_recovery_test.go`
- `codex_runtime_recovery_test.go`
- `compact_more_test.go`
- `submission_queue_claude_test.go`
- `steer_more_test.go`
- `turn_binding_test.go`
- `goal_test.go`
- `codex_plan_mode_exit_test.go`

## root 中剩余的测试辅助文件

还有少量 root test helper / export 文件继续存在，例如:

- `pendingforms_test_helpers_test.go`
- `coverage_wrappers_test.go`

它们存在的前提仍然是服务于 root contract 或跨包 critical path；如果后续可以迁回 owner package，就应该继续迁。

## 已经在 owner package 的测试模式

当前已经稳定采用的 owner-local 模式包括:

- `submission/submission_pure_test.go`
- `serverrequest/input_service_test.go`
- `serverrequest/request_id_test.go`
- `delivery/reply_card_split_pure_test.go`
- `pathpick/render_test.go`
- `workspacecmd/thread_service_test.go`
- `servicetiercmd/service_test.go`
- `features/registry_test.go`

这类测试说明默认策略已经很明确：纯逻辑、纯 helper、纯 rendering、窄 service 行为都应直接放在 owning package。

## Guardrails

- 新增测试默认放 owning package。
- 只有同时满足“验证 root 入口 / registry contract / cross-package critical path / state-machine guard”之一时，才允许新增 root `internal/app` 测试。
- 新的 root-local 纯 helper 测试应视为结构回退。
