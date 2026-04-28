# Root `internal/app` 测试文件最终审计

> Phase 11 — 2026-04-28

## 审计结论

root `internal/app` 当前保留 **83 个测试文件**。

本次 Phase 11 审计只按执行计划允许的 4 类职责保留 root 测试：

1. Feishu event routing 整体入口
2. command/menu/action registry 直达性
3. 跨 package 的 critical path
4. state-machine 契约 guard

本次收口后的结论是：

- 已迁出 3 个明显 owner-local 的 root 测试文件。
- root 不再把“Feature-Local Integration / Card Rendering / Infrastructure / Test-Helper Exports”当作独立保留类别。
- **64/83** 个 root 测试文件仍然通过 `newTestApp(t)` 构造完整 `App`，这些文件统一按“整体入口 / 跨 package critical path / 状态机 guard”重新归类，而不是继续视为 feature-local unit test。
- 剩余 **19/83** 个非 `newTestApp(t)` 测试文件，也只作为 root-owned routing / registry / protocol guard 的窄测试保留，不再单独扩张出新的类别。

## 测试迁移记录

| 原文件 | 新位置 | 说明 |
| --- | --- | --- |
| `session_status_test.go` | `submission/submission_pure_test.go` | `ShouldStartNextSubmissionAsync` 纯逻辑迁入 `submission` owner |
| `pending_forms_more_test.go` | `serverrequest/input_service_test.go` | pending text dispatch helper 迁入 `serverrequest` owner |
| `request_id_more_test.go` | `serverrequest/request_id_test.go` | request-id 纯 helper 迁入 `serverrequest` owner |

## 新增 owner-local 测试

| 文件 | 包 | 说明 |
| --- | --- | --- |
| `submission/submission_pure_test.go` | `submission` | 16 个纯函数测试，覆盖 submission owner 导出的纯逻辑 |
| `upgradecmd/upgrade_pure_test.go` | `upgradecmd` | 8 个纯函数测试，覆盖 upgrade owner 导出的纯逻辑 |
| `serverrequest/input_service_test.go` | `serverrequest` | pending text dispatch root helper 迁移后的 owner-local 守卫 |
| `serverrequest/request_id_test.go` | `serverrequest` | request-id helper root helper 迁移后的 owner-local 守卫 |

## Category 1: Feishu Event Routing 整体入口

这类测试保留在 root，是因为它们直接验证 Feishu 入口、异步卡片回调、通知路由和前台级 app/service 组合边界。

共 **14** 个文件：

- `actions_dispatch_more_test.go`
- `app_branches_more_test.go`
- `backend_routing_test.go`
- `card_action_async_test.go`
- `coverage_branches_test.go`
- `coverage_wrappers_test.go`
- `delivery_more_test.go`
- `feishu_notifier_test.go`
- `frontend_idle_test.go`
- `inbound_deduper_test.go`
- `notifications_branches_more_test.go`
- `notifications_more_test.go`
- `notifications_test.go`
- `service_more_test.go`

## Category 2: Command/Menu/Action Registry 直达性

这类测试保留在 root，是因为它们直接守卫“菜单能力必须有 direct command entrypoint”和 action/feature 注册表唯一性。

共 **13** 个文件：

- `action_registry_contracts_test.go`
- `actions_more_more_test.go`
- `actions_test.go`
- `card_demo_more_test.go`
- `commands_test.go`
- `debugview_test_exports_test.go`
- `feature_registry_test.go`
- `history_test_exports_test.go`
- `menu_command_direct_access_test.go`
- `model_config_more_test.go`
- `model_config_test.go`
- `service_tier_more_test.go`
- `thread_test_exports_test.go`

说明：

- `debugview_test_exports_test.go`
- `history_test_exports_test.go`
- `thread_test_exports_test.go`

这 3 个文件只是为 bucket-2 的 direct-command / menu contract 测试暴露子包符号，不构成单独测试类别。

## Category 3: 跨 Package Critical Path

这类测试虽然很多带有 feature 主题，但保留原因不是“root 拥有该 feature”，而是它们依赖完整 `App` 组合根、真实 store/session/runtime wiring，验证的是跨 package 的端到端路径。

共 **39** 个文件：

- `app_more_test.go`
- `backend_core_more_test.go`
- `backend_failure_test.go`
- `backend_selection_test.go`
- `claude_core_test.go`
- `claude_dynamic_tool_rendering_test.go`
- `claude_history_test.go`
- `claude_permission_config_test.go`
- `claude_session_catalog_test.go`
- `claude_streaming_segment_test.go`
- `deps_test.go`
- `download_more_test.go`
- `final_card_patch_test.go`
- `helpers_more_test.go`
- `history_more_test.go`
- `local_file_links_more_test.go`
- `maintenance_more_test.go`
- `outbound_cards_more_test.go`
- `path_picker_test.go`
- `pending_inputs_more_test.go`
- `pending_inputs_test.go`
- `quiet_mode_test.go`
- `quiet_working_card_more_test.go`
- `quiet_working_card_test.go`
- `reply_card_split_more_test.go`
- `reply_card_split_test.go`
- `review_critical_test.go`
- `review_test.go`
- `runtime_maintenance_more_test.go`
- `session_lineage_test.go`
- `skills_test.go`
- `token_usage_test.go`
- `turn_item_cards_more_test.go`
- `turn_stream_more_test.go`
- `turn_stream_test.go`
- `upgrade_isolation_test.go`
- `upgrade_more_test.go`
- `upgrade_test.go`
- `workspace_threads_more_test.go`

## Category 4: State-Machine 契约 Guard

这类测试保留在 root，是因为它们直接守卫 turn lifecycle、approval/server request、resume/recovery、retry/interrupt/compact 等协议敏感边界。

共 **17** 个文件：

- `approval_policy_turn_test.go`
- `auto_retry_test.go`
- `claude_runtime_warmup_test.go`
- `codex_runtime_recovery_test.go`
- `codex_turn_recovery_test.go`
- `codex_ws_dispatch_test.go`
- `compact_more_test.go`
- `critical_paths_more_test.go`
- `critical_paths_test.go`
- `item_started_server_request_test.go`
- `pending_forms_branches_test.go`
- `protocol_business_logic_test.go`
- `server_request_reply_error_test.go`
- `state_machine_contracts_test.go`
- `steer_more_test.go`
- `submission_queue_claude_test.go`
- `turn_binding_test.go`

## 下一轮约束

- Phase 11 之后，新增测试默认放 owning package。
- 只有满足下面任一条件，才允许新增 root `internal/app` 测试：
  - 它直接验证 Feishu 入口或前台级 routing
  - 它直接验证 command/menu/action registry 的直达性
  - 它必须依赖完整 `App` 组合根，验证跨 package critical path
  - 它是协议状态机 guard
- 任何新的 root-local 纯 helper 测试，都应视为结构回退。
