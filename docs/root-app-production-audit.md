# Root `internal/app` 生产文件最终审计

> Phase 11 — 2026-04-28

## 审计结论

root `app` 包保留 **157 个生产文件**，共 **14,470 行**。

按 Phase 11 规范分为 4 类:
- **A. Bootstrap / Composition** — 6 文件
- **B. Routing / Registries** — 20 文件
- **C. Protocol-Sensitive Orchestration** — 29 文件
- **D. Minimal Glue** — 102 文件

## Category A: Bootstrap / Composition (6 files, ~1,420 lines)

初始化 App、注入依赖、启动服务。

| 文件 | 行数 | 职责 |
|------|------|------|
| `app.go` | 309 | App struct, constructor, Start/Stop lifecycle |
| `service.go` | 112 | Service 层, frontend app factory |
| `app_deps.go` | 68 | 依赖注入 factories |
| `accessors.go` | 193 | App accessor methods (State, Config, Feishu, etc.) |
| `deps.go` | 48 | 全局 dependency factories |
| `health.go` | 24 | health check |

## Category B: Routing / Registries (20 files, ~2,630 lines)

路由 Feishu 事件、命令、card action 到 handler。

| 文件 | 行数 | 职责 |
|------|------|------|
| `feishu_event_router.go` | 188 | Feishu 事件路由总入口 |
| `command_registry.go` | 139 | command 注册表 |
| `commands.go` | 97 | command dispatch |
| `menu_actions.go` | 224 | menu action handlers |
| `menu_command_bridge.go` | 65 | menu command 桥接 |
| `menu_nav.go` | 49 | menu 导航 |
| `menu_registry.go` | 86 | menu 注册表 |
| `menu_specs.go` | 149 | menu 规格定义 |
| `menu_types.go` | 46 | menu 类型定义 |
| `action_registry.go` | 82 | card action 注册表 |
| `action_registry_maintenance.go` | 54 | action registry 维护 |
| `action_registry_menu.go` | 52 | menu action 注册 |
| `action_registry_pending.go` | 60 | pending action 注册 |
| `action_registry_workspace.go` | 59 | workspace action 注册 |
| `backend_core.go` | 101 | backend core routing |
| `backend_selection.go` | 167 | backend 选择逻辑 |
| `backend_deps.go` | 45 | backend 依赖 |
| `feature_registry_bindings.go` | 188 | feature registry bindings |
| `feature_registry_bindings_menu_core.go` | 92 | menu core features |
| `feature_registry_bindings_system.go` | 150 | system features |

## Category C: Protocol-Sensitive Orchestration (29 files, ~4,080 lines)

管理 turn 生命周期、submission 状态机、server request、runtime 交互。
这些文件处理协议敏感的时序和状态转换，保留在 root 以维护端到端可见性。

| 文件 | 行数 | 职责 |
|------|------|------|
| `turn_binding.go` | 149 | turn-submission binding (delegates to turnbinding.Tracker) |
| `turn_stream.go` | 156 | turn stream 管理 |
| `turn_item_cards.go` | 176 | turn item card 构建 |
| `turn_item_state.go` | 162 | turn item 状态管理 |
| `turnitem_helpers.go` | 76 | turn item helpers |
| `turn_lifecycle.go` | 128 | turn lifecycle 管理 |
| `turnlifecycle_bindings.go` | 103 | turn lifecycle bindings |
| `turnstream_bindings.go` | 62 | turn stream bindings |
| `submission_queue.go` | 91 | submission queue coordinator |
| `submission_status.go` | 26 | submission status helpers |
| `submission_workflow.go` | 95 | submission workflow |
| `submission_bindings.go` | 303 | submission package bindings |
| `server_request_state.go` | 98 | server request state |
| `server_request_delivery_scaffold.go` | 109 | server request delivery |
| `serverrequest_bindings.go` | 257 | server request bindings |
| `pending_inputs.go` | 93 | pending input 管理 |
| `approval_cards.go` | 89 | approval card 构建 |
| `approval_summaries.go` | 70 | approval summaries |
| `claude_runtime.go` | 148 | Claude runtime 管理 |
| `claude_thread_binding.go` | 104 | Claude thread binding |
| `claude_streaming_segment.go` | 133 | Claude streaming |
| `clauderuntime_bindings.go` | 112 | Claude runtime bindings |
| `codex_event_router.go` | 107 | Codex event routing |
| `codex_runtime_recovery.go` | 120 | Codex runtime recovery |
| `codex_turn_recovery.go` | 89 | Codex turn recovery |
| `codex_auto_retry.go` | 88 | Codex auto retry |
| `conversation_backend.go` | 126 | conversation backend |
| `conversation_backend_fork.go` | 66 | conversation fork |
| `convbackend_bindings.go` | 231 | convbackend bindings |

## Category D: Minimal Glue (102 files, ~6,340 lines)

薄 wrapper、纯 helper、type alias、单一职责工具函数。
这些文件大部分已将核心逻辑下沉到子包，root 只保留 adapter/binding 层。

### Backend Runtime (7 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `backend_runtime.go` | 142 | backend runtime interface |
| `backend_runtime_claude.go` | 170 | Claude runtime adapter |
| `backend_runtime_codex.go` | 140 | Codex runtime adapter |
| `backend_runtime_helpers.go` | 68 | runtime helpers |
| `backend_state_service.go` | 69 | backend state service |
| `backend_upgrade_state.go` | 62 | upgrade state adapter |
| `backend_configuration_helpers.go` | 61 | configuration helpers |

### Backend Actions & Maintenance (5 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `backend_actions.go` | 93 | backend action handlers |
| `backend_failure.go` | 234 | backend failure handling |
| `backend_maintenance_scaffold.go` | 141 | maintenance scaffold |
| `backendcaps_helpers.go` | 49 | backend capabilities helpers |
| `startup_recovery.go` | 189 | startup recovery |

### Feature Registry (4 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `feature_registry_bindings_thread_workspace.go` | 191 | thread/workspace features |
| `feature_registry_bindings_tools.go` | 186 | tool features |
| `feature_registry_bindings_system.go` | (in B) | system features |
| `feature_registry_bindings_menu_core.go` | (in B) | menu core features |

### Card & UI (12 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `card_helpers.go` | 72 | card helper functions |
| `card_action_async.go` | 105 | async card actions |
| `card_demo.go` | 58 | card demo |
| `outbound_cards.go` | 94 | outbound card 构建 |
| `reply_card_split.go` | 114 | markdown table 分割 |
| `reply_chunk_delivery.go` | 79 | reply chunk delivery |
| `quiet_working_card.go` | 125 | quiet working card |
| `quiet_working_types.go` | 36 | quiet working types |
| `final_card_patch.go` | 72 | final card patch |
| `finalcardpatch_bindings.go` | 44 | final card patch bindings |
| `local_file_links.go` | 58 | local file links |
| `frontend_card_notifications.go` | 78 | frontend card notifications |

### Workspace (10 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `workspacecmd_bindings.go` | 97 | workspace command bindings |
| `workspacecmd_bindings_config.go` | 80 | workspace config bindings |
| `workspacecmd_bindings_management.go` | 162 | workspace management bindings |
| `workspacecmd_bindings_render.go` | 183 | workspace render bindings |
| `workspacecmd_bindings_thread.go` | 120 | workspace thread bindings |
| `workspace_creation.go` | 104 | workspace creation |
| `workspace_creation_clone.go` | 79 | workspace clone |
| `workspace_creation_render.go` | 81 | workspace creation render |
| `workspace_delete.go` | 48 | workspace delete |
| `workspace_threads.go` | 93 | workspace threads |

### Review (4 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `review.go` | 98 | review command |
| `review_bindings.go` | 150 | review bindings |
| `review_callback_async.go` | 81 | review async callback |
| `review_forms.go` | 95 | review forms |
| `review_git.go` | 76 | review git operations |

### Upgrade (8 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `upgrade.go` | 171 | upgrade command |
| `upgrade_local.go` | 76 | local upgrade |
| `claude_upgrade.go` | 75 | Claude upgrade |
| `claude_upgrade_actions.go` | 83 | Claude upgrade actions |
| `claude_upgrade_render.go` | 87 | Claude upgrade render |
| `claude_upgrade_runtime.go` | 251 | Claude upgrade runtime |
| `codex_upgrade.go` | 79 | Codex upgrade |
| `codex_upgrade_actions.go` | 84 | Codex upgrade actions |
| `codex_upgrade_render.go` | 80 | Codex upgrade render |
| `codex_upgrade_runtime.go` | 169 | Codex upgrade runtime |

### History & Debug (5 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `history_bindings.go` | 78 | history bindings |
| `claude_history.go` | 85 | Claude history |
| `debugview_bindings.go` | 142 | debug view bindings |
| `session_debug.go` | 57 | session debug |
| `status_panel.go` | 68 | status panel |

### Model Config & Skills (5 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `model_config.go` | 101 | model config |
| `modelconfig_exports.go` | 56 | model config exports |
| `skills_bindings.go` | 72 | skills bindings |
| `claude_permission_config.go` | 85 | Claude permission config |
| `claude_model_config.go` | 78 | Claude model config |

### Session & Lifecycle (6 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `session_active_ops.go` | 42 | session active operations |
| `session_lineage.go` | 92 | session lineage |
| `session_status.go` | 17 | session status (delegates to submission) |
| `autoretry_bindings.go` | 52 | auto retry bindings |
| `compact_bindings.go` | 55 | compact bindings |
| `compact.go` | 85 | compact command |

### Other Glue (22 files)

| 文件 | 行数 | 说明 |
|------|------|------|
| `attachments.go` | 56 | attachment helpers |
| `attention_mentions.go` | 42 | attention mentions |
| `claude_support.go` | 194 | Claude support helpers |
| `claude_session_catalog.go` | 101 | Claude session catalog |
| `conversation_backend_menu.go` | 78 | conversation menu |
| `conversation_terms.go` | 45 | conversation terms |
| `delivery.go` | 170 | delivery service |
| `final_delivery.go` | 66 | final delivery |
| `feishu_notifier.go` | 102 | Feishu notifier |
| `fork.go` | 56 | fork operations |
| `frontend_helpers.go` | 43 | frontend helpers |
| `frontend_idle.go` | 80 | frontend idle state |
| `inbound_deduper.go` | 63 | inbound deduplication |
| `markdown_normalize.go` | 38 | markdown normalization |
| `merge_forward.go` | 45 | merge forward handling |
| `message_links.go` | 52 | message links |
| `path_display.go` | 39 | path display |
| `path_picker_actions.go` | 190 | path picker actions |
| `path_picker_render.go` | 86 | path picker render |
| `quiet_mode.go` | 143 | quiet mode |
| `request_id.go` | 58 | request ID helpers |
| `runtime_maintenance.go` | 97 | runtime maintenance |
| `runtime_types.go` | 34 | runtime types |
| `service_tier.go` | 155 | service tier |
| `tool_user_input_forms.go` | 68 | tool user input forms |
| `ui_warning.go` | 34 | UI warning |
| `action_services_workspace_thread.go` | 88 | workspace thread action services |
| `thread_feature_actions.go` | 80 | thread feature actions |
| `workspace_feature_actions.go` | 72 | workspace feature actions |
| `workspace_menu.go` | 67 | workspace menu |

## 大文件清单

root `app` 中超过 200 行的生产文件:

| 文件 | 行数 | 类别 | 保留原因 |
|------|------|------|----------|
| `app.go` | 309 | Bootstrap | App 核心 struct 和生命周期 |
| `submission_bindings.go` | 303 | Orchestration | submission 包的 adapter 层，30+ 接口方法 |
| `serverrequest_bindings.go` | 257 | Orchestration | serverrequest 包的 adapter 层 |
| `claude_upgrade_runtime.go` | 251 | Glue | Claude upgrade runtime 管理 |
| `backend_failure.go` | 234 | Glue | backend failure handling |
| `convbackend_bindings.go` | 231 | Orchestration | convbackend 包的 adapter 层 |
| `menu_actions.go` | 224 | Routing | menu action handlers |
| `claude_support.go` | 194 | Glue | Claude support helpers |
| `accessors.go` | 193 | Bootstrap | App accessor methods |
| `feature_registry_bindings_thread_workspace.go` | 191 | Glue | thread/workspace features |
| `path_picker_actions.go` | 190 | Glue | path picker actions |
| `startup_recovery.go` | 189 | Glue | startup recovery |
| `feishu_event_router.go` | 188 | Routing | Feishu 事件路由 |
| `feature_registry_bindings.go` | 188 | Routing | feature registry bindings |
| `feature_registry_bindings_tools.go` | 186 | Glue | tool features |
| `workspacecmd_bindings_render.go` | 183 | Glue | workspace render bindings |
| `turn_item_cards.go` | 176 | Orchestration | turn item card 构建 |
| `upgrade.go` | 171 | Glue | upgrade command |
| `delivery.go` | 170 | Glue | delivery service |
| `backend_runtime_claude.go` | 170 | Glue | Claude runtime adapter |
| `codex_upgrade_runtime.go` | 169 | Glue | Codex upgrade runtime |
| `backend_selection.go` | 167 | Routing | backend 选择 |
| `replycontinuation_bindings.go` | 163 | Orchestration | reply continuation bindings |
| `workspacecmd_bindings_management.go` | 162 | Glue | workspace management |
| `turn_item_state.go` | 162 | Orchestration | turn item 状态 |
| `turn_stream.go` | 156 | Orchestration | turn stream |
| `service_tier.go` | 155 | Glue | service tier |
| `review_bindings.go` | 150 | Glue | review bindings |
| `feature_registry_bindings_system.go` | 150 | Routing | system features |
| `turn_binding.go` | 149 | Orchestration | turn binding |
| `menu_specs.go` | 149 | Routing | menu specs |
| `claude_runtime.go` | 148 | Orchestration | Claude runtime |
| `threadmenu_bindings.go` | 145 | Glue | thread menu bindings |
| `quiet_mode.go` | 143 | Glue | quiet mode |
| `debugview_bindings.go` | 142 | Glue | debug view bindings |
| `backend_runtime.go` | 142 | Glue | backend runtime interface |
| `backend_maintenance_scaffold.go` | 141 | Glue | maintenance scaffold |
| `notifications.go` | 140 | Routing | notification routing |
| `backend_runtime_codex.go` | 140 | Glue | Codex runtime adapter |
| `command_registry.go` | 139 | Routing | command registry |

## 下一轮单独计划

Phase 11 在这里停止做“最终审计”，不继续顺手展开下一轮根包瘦身。

当前 root `app` 仍有 **157 个生产文件 / 14,470 行**。这已经超过“日常顺手收一收”能安全完成的范围，因此后续 root 收敛必须按单独计划执行，而不是在零散 feature PR 中继续自然膨胀或被动回流。

下一轮单独计划见：

- [docs/root-app-next-wave-plan.md](/home/yuhuan/feidex/docs/root-app-next-wave-plan.md)

该计划明确列出了：

1. `workspace*` / path-picker 文件族的下沉顺序
2. `upgrade*` / `review*` / `history*` / `debugview*` 的 owner 收口顺序
3. `backend runtime` / startup-recovery / failure glue 的压缩顺序
4. 与这些生产文件收敛同步进行的剩余 root 测试迁移顺序
