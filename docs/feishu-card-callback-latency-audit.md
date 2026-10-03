# 飞书卡片回调延迟审计

> 更新时间：2026-10-03

本文只审计 `card.action.trigger` 的同步 callback ack 路径。目标是保证 callback 快速返回，把 git、网络、真实 Feishu API、长 CLI/runtime 操作放到异步工作，并用 card patch 或 follow-up 报告结果。slash command 的完整耗时和已脱离 callback 的后台任务不计入同步 ack 风险。

主要入口与实现：

- `internal/feishu/events.go`、`internal/feishuapp/action_registry.go`：事件注册和 callback ack。
- `internal/feishuapp/input_dispatcher.go`、`internal/feishuapp/card_action_async.go`、`internal/feishuapp/menu_actions.go`：typed action dispatch 与异步边界。
- `internal/application/cardaction/dispatcher.go`、`internal/application/features/`：action 分类、能力 gate 和 command/menu 语义。
- `internal/adapter/feishu/goalcmd/`、`planmode/`、`reviewcmd/`、`upgradecmd/`、`workspacecmd/`、`debugviewcmd/`：Feishu command/card adapter。
- `internal/adapter/feishu/outbound/`、`delivery/`、`finalcardpatch/`：semantic effect 执行和卡片 patch。

## 判断标准

从 action handler 开始，只追踪第一次 `runAsync`、`CompleteAsync*`、入队或 pending 保存之前的代码。以下属于重路径，必须先 ack：

- `git clone/fetch/log/diff/status/for-each-ref` 或其他规模相关进程。
- 外部网络、真实 Feishu API、文件分享和可能阻塞的 backend/CLI control request。
- 任何依赖仓库大小、网络、磁盘或 CLI 状态而可能明显波动的工作。

参数校验、pending claim、内存状态转换、卡片渲染、入队和本地控制响应属于轻路径。`commandCaptureClient` 的 capture 不是真实 Feishu API；`ShareLocalFile`、outbound transport 和 backend control request 仍按真实外部操作审计。

## 当前结论

### 重流程但已异步保护

| Action | 慢操作 | 保护 |
| --- | --- | --- |
| `menu.review.uncommitted/base/commit` | git 状态、分支或提交查询 | preparing card 先 ack，后台执行并 patch |
| `review.base.select`、`review.commit.select` | 重新查询 git 目标 | 后台刷新并 patch |
| `review.form.submit`（base/commit） | git 解析、diff 或 review/start | 后台启动 review 并 patch/follow-up |
| `menu.review.*`、`upgrade.dev`、`menu.upgrade` | 远端 release 或 review 工作流 | `CompleteAsync*` / preparing card |
| `codex_upgrade.check/prepare`、`claude_upgrade.check/prepare` | 自升级探测与准备 | 后台执行并报告结果 |
| `menu.plan`、`menu.goal`、`goal.*` | Codex collaboration/goal RPC | toast/card 先 ack，后台执行并 patch/text |
| `workspace.clone.submit` | git clone | callback 只做 preflight，后台 clone |
| `path_picker.confirm` 的 `download_file` | 真实 Feishu 文件分享 | 后台分享并 patch |
| `menu.interrupt`（Claude） | CLI interrupt control request | preparing card 后台执行并 patch |
| `menu.compact`（Claude） | 长 compact workflow | preparing card 后台入队 |
| `workspace.use.*`、`workspace.new.submit` | 启动 backend/CLI 并绑定 thread | callback ack 后异步 thread binding |

### 轻路径或明确排除

- approval、permissions、`user_input.answer`、`pending_form.cancel`：只做本地 pending claim/reply，真实 `serverRequest/resolved` 仍由 backend lifecycle 处理。
- `menu.download`、`menu.codex_upgrade`、`codex_upgrade.refresh`、`claude_upgrade.refresh`：只打开 selector 或做本地 probe。
- Codex 的 `menu.model`、`model.config.*`、`menu.skills`、`skills.*`、`menu.history`、`history.*`、`menu.new`、`menu.fork`、`thread.resume.select`：当前确认是本地快速路径；若新增 network/git/真实 Feishu 调用，必须重新审计。

Claude 的模型/effort/permission/auxiliary-model card action 如果触发 runtime control request 或重启，会先保存 desired configuration，再异步应用；无真实 `MessageID` 的编程式 fallback 可以同步执行，不属于正常 Feishu callback ack 路径。

## 持续守卫

- 新 action 默认按 `fast callback ack -> async work -> patch/follow-up` 设计。
- callback 中不得同步执行 git、网络、真实 Feishu API 或长 CLI/runtime 往返。
- 异步工作如果修改 session/submission/turn，必须通过 frontend-owned session actor；frontend-wide maintenance/recovery 任务按 session 重新投递。
- 变更 `review`、`upgrade`、`download`、`clone`、`compact`、`interrupt`、`goal`、`plan` 或 Claude control-response 边界时，同时更新本文和对应测试。

已有行为由 `internal/feishuapp/card_action_async_test.go`、`internal/feishuapp/claude_permission_menu_async_test.go`、`internal/feishuapp/async_user_input_test.go`、workspace/review/upgrade 测试覆盖；事件注册与 handler 一致性由 `internal/feishu/events_test.go` 守卫。
