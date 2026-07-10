# `internal/app` 重构现状

更新时间: 2026-07-10

相关约束文档:

- [DEVELOPER.md](/home/yuhuan/feidex/DEVELOPER.md)
- [docs/codex-app-server-state-machine-audit.md](/home/yuhuan/feidex/docs/codex-app-server-state-machine-audit.md)
- [docs/app-package-boundaries.md](/home/yuhuan/feidex/docs/app-package-boundaries.md)
- [docs/backend-layering.md](/home/yuhuan/feidex/docs/backend-layering.md)
- [docs/root-app-production-audit.md](/home/yuhuan/feidex/docs/root-app-production-audit.md)
- [docs/root-app-test-audit.md](/home/yuhuan/feidex/docs/root-app-test-audit.md)

## 结论

`internal/app` 的阶段化重构已经完成。旧的 phase / wave 执行计划不再作为当前工作的 source of truth；现在以边界文档、状态机审计和 root 现状审计为准。

截至 2026-07-10:

- root `internal/app` 保留 `147` 个生产文件，共 `16,523` 行。
- root `internal/app` 保留 `89` 个测试文件，共 `27,405` 行。
- `internal/app` 目录下已有 `59` 个直接子包。

这说明重构已经从“执行计划阶段”进入“稳定边界阶段”。后续工作只做增量收敛，不再继续维护旧的阶段编号文档。

这次 root 行数增长主要来自仍然需要 frontend-scoped 状态锚点的新增协议/产品胶水，例如 thread goal、MCP send bridge、Codex plan-mode exit 和 Plan 模式配置；这些文件不改变 root 的组合根定位，但后续继续增长时应优先抽出 owner-local helper。

## 已稳定下来的结构

- `appstate` 已成为 app 层访问持久化状态的 owner；不再回到 `appStateFacade` 式的万能代理。
- backend 差异主要收敛在 `backend`、`backendcaps`、`convbackend`、`clauderuntime`、`claudesession`、`claudesupport`、`codexruntime` 等边界内。
- owner-local 纯逻辑和纯渲染继续下沉到 `delivery`、`pathpick`、`review`、`turnitem`、`features`、`serverrequest`、`submission`、`workspacecmd`、`reviewcmd`、`upgradecmd` 等子包。
- root `internal/app` 现在主要保留组合根、Feishu 入口、frontend-scoped lifecycle、以及必须对照 Codex app-server 审计的协议敏感编排。

## 当前 root `internal/app` 允许保留的内容

- 组合与启动: `app.go`, `service.go`, `deps.go`, `accessors.go`, `app_deps.go`
- 入口路由与注册表: `feishu_event_router.go`, `notifications.go`, `commands.go`, `command_registry.go`, `menu_*.go`, `action_registry*.go`, `feature_registry_bindings*.go`
- 协议敏感编排: `submission_*`, `turn_*`, `server_request_state.go`, `pending_inputs.go`, `goal.go`, `plan_mode.go`, `codex_plan_mode_exit.go`, `mcp_service.go`, `claude_runtime.go`, `codex_*recovery.go`, `compact.go`
- 极薄 glue: `*_bindings.go`, `backend_runtime*.go`, `backend_selection.go`, `review_bindings.go`, `workspacecmd_bindings*.go`, `maintenance_bindings.go`

## 继续适用的约束

- 不新增新的 root-level alias / shim / placeholder wrapper 文件族。
- owner-local 纯逻辑、纯格式化、纯渲染、纯 DTO 不放回 root `internal/app`。
- 任何触及 approvals、turn/thread lifecycle、compaction、review、tool input、server request 的改动，都要对照状态机审计并补测试。
- root 测试只保留 routing / registry、cross-package critical path、state-machine guard，以及少量只为这些 contract 服务的 test exports。

## 仍然值得继续收敛的区域

这些是当前 root 中仍偏重的区域，但它们属于日常边界优化，不再单独维护历史 phase 计划文档:

- workspace / path-picker glue
- upgrade / review / history / debug bindings
- backend runtime / recovery / maintenance glue
- goal / MCP / plan-mode exit 中可下沉的 owner-local helper
- 大型 binding 文件，例如 `submission_bindings.go`、`serverrequest_bindings.go`、`convbackend_bindings.go`

当这些区域发生真实需求改动时，优先顺手把 owner-local 逻辑继续压回对应子包；不要为了维持旧计划而额外保留“下一轮”文档。
