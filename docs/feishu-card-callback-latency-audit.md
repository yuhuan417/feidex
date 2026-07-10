# 飞书卡片回调延迟审计

> 更新时间: 2026-07-10
>
> 目的:
> - 收口当前 Feidex 的飞书卡片回调延迟风险集合。
> - 给后续新增卡片动作提供统一判断标准。
> - 区分“同步 ack 路径风险”和“业务上是重流程，但当前已异步保护”。

主要代码来源:

- `internal/app/card_action_async.go`
- `internal/app/menu_actions.go`
- `internal/app/action_registry_menu.go`
- `internal/app/action_registry_workspace.go`
- `internal/app/action_registry_maintenance.go`
- `internal/app/action_registry_pending.go`
- `internal/app/feature_registry_bindings_tools.go`
- `internal/app/feature_registry_bindings_thread_workspace.go`
- `internal/app/goal_bindings.go`
- `internal/app/goalcmd/service.go`
- `internal/app/plan_mode_bindings.go`
- `internal/app/planmode/service.go`
- `internal/app/planmode/exit.go`
- `internal/app/reviewcmd/async_menu.go`
- `internal/app/reviewcmd/service.go`
- `internal/app/review/git.go`
- `internal/app/delivery/download.go`
- `internal/app/path_picker_actions.go`
- `internal/app/upgradecmd/service.go`
- `internal/app/backend_maintenance_scaffold.go`
- `internal/app/codex_upgrade_actions.go`
- `internal/app/claude_upgrade_actions.go`
- `internal/app/compact.go`
- `internal/app/threadmenu/service.go`
- `internal/app/workspace/clone.go`
- `internal/app/workspacecmd/management_service.go`
- `internal/app/claude_runtime.go`
- `internal/app/backend/actions.go`
- `internal/app/serverrequest/adapter.go`
- `internal/claudecli/session.go`

## 审计范围

- 本文只审计飞书同步卡片回调入口 `dispatchCardAction()` 这一段 ack 路径。
- 不审计 slash 命令全文执行耗时，也不审计后台 goroutine、队列消费、异步 patch card 的耗时。
- 这里的“风险”指“有较大概率把同步卡片 ack 路径拖慢”，不是指业务本身一定失败。
- backend 相关结论按“当前 frontend 已固定到该 backend”来理解。

## 判断标准

新增或修改卡片动作时，先按下面顺序判断。

### 1. 只看第一次异步边界之前

- 从 action registry 对应 handler 开始 trace。
- 只看第一次异步边界之前的逻辑:
  - `go func()`
  - `runAsync(...)`
  - 只做入队、存 pending、返回 preparing card
- 一旦跨过这条边界，后续耗时不再算“同步 ack 风险”，但仍要记录成“重流程，已异步保护”。

### 2. 命中下列任一条件，就按重路径处理

- 同步执行 `git` 子进程，尤其是 `clone`、`fetch`、`log`、`diff`、`status`、`for-each-ref`。
- 同步执行外部网络 I/O。
- 同步调用真实飞书 API。
- 同步发起可能阻塞的 backend runtime / CLI 交互，而首个用户可见响应本来可以先返回。
- 行为有时很快、有时明显依赖仓库规模、网络、磁盘、CLI 状态时，也按重路径保守处理。

### 3. 命中下列条件，可视为轻路径

- 只做本地参数校验、pending/state 更新、卡片渲染、入队。
- 只做本地内存中的 pending reply / control-response 投递。
- 只读本地小量状态，不发外部网络、不起长子进程、不打真实飞书 API。

### 4. 两个容易误判的实现细节

- `completeMenuCommand()` 通过 `commandCaptureClient` 跑 slash 命令时，`ReplyText`、`ReplyCard`、`SendCard`、`PatchCard` 默认只是本地 capture，不是真正回飞书。
- 当前菜单桥接里，`ShareLocalFile()` 才会透传到真实飞书；因此“看起来像命令里回复了卡片”不等于“回调里真的打了飞书 API”。

### 5. Claude 专项补充判断

- “直接打 Claude CLI / runtime” 和 “给一个已阻塞的 Claude 会话回控制响应” 不是一回事。
- 前者要单独评估是否可能拖慢 ack。
- 后者如果只是把审批、问答、计划反馈投递回本地 pending channel，通常按轻路径处理。

## 当前收口结论

本文把动作分成三类:

- `同步风险`: 当前卡片回调本身就可能拖慢 ack。
- `重流程但已异步保护`: 业务上是长操作，但当前回调已先返回。
- `轻路径/排除`: 当前不纳入 timeout 重点集合。

## Codex

用户本轮明确要求: `Codex` 侧主要关注 `git`、外部 `network`、真实嵌套飞书调用；普通 thread/workspace 设置不作为重点集合。

另外，用户已明确确认:

- `Codex` 本地 app-server 的 `model`、`skills`、`history`、`thread/workspace` 这批本地 RPC 返回很快。
- 因此这些 `Codex` 本地设置 / 切换类动作，当前按快路径处理，不纳入本轮异步化改造集合。
- 除非它们第一次异步边界之前新增了 `git`、远端 `network`、真实飞书调用，或者出现新的明确慢证据，否则不要反复把它们拉回待改集合。

### 同步风险

截至 2026-07-10，当前没有已知仍在 Codex 卡片 callback 同步 ack 路径执行 `git`、远端 network 或真实飞书 API 的动作。

补充说明:

- 下面 `review.*` / `upgrade.dev` 仍然是重流程，但已经有 preparing card + async patch 保护。
- 这些异步保护依赖真实卡片回调带 `MessageID`；无 `MessageID` 的测试/命令式 fallback 可以同步执行，不属于 `dispatchCardAction()` 的正常 ack 路径。

### 重流程但已异步保护

| Action | 重流程来源 | 当前保护方式 |
| --- | --- | --- |
| `menu.review.uncommitted` | `git rev-parse` + `git status` | `ReviewCompleteAsyncCommandAction` 先返回 preparing card，后台执行并 patch |
| `menu.review.base` | 列 branch，走 `git for-each-ref` | `ReviewCompleteAsyncCommandAction` 先返回 preparing card，后台执行并 patch |
| `menu.review.commit` | 列 commit，走 `git log` | `ReviewCompleteAsyncCommandAction` 先返回 preparing card，后台执行并 patch |
| `review.base.select` | 重新列 branch | `ReviewCompleteAsyncRenderedCardAction` 先返回 preparing card，后台刷新并 patch |
| `review.commit.select` | 可能重新列 commit 以补 title | `ReviewCompleteAsyncRenderedCardAction` 先返回 preparing card，后台刷新并 patch |
| `review.form.submit` 的 `base` / `commit` 模式 | `base` 模式会跑 `rev-parse/status/diff`；`commit` 模式会跑 `rev-parse/log -1` | `ReviewCompleteAsyncRenderedCardAction` 先返回 preparing card，后台启动 review 并 patch |
| `upgrade.dev` | 查远端 release | `completeAsyncCommandAction` 先返回 preparing card，后台执行并 patch |
| `menu.upgrade` | 查远端 release | 先返回 preparing card，后台执行 |
| `codex_upgrade.check` | 探测 `codex update` 自升级命令 | 先返回 preparing card，后台执行 |
| `codex_upgrade.prepare` | 同上 | 先返回 preparing card，后台执行 |
| `menu.plan` | 可能调用 Codex `collaborationMode/list` / `model/list` | 先返回 toast，后台执行原 `/plan` 命令并 patch 原卡或发送文本结果 |
| `menu.goal` | 调用 Codex `thread/goal/get` | 先返回 toast，后台执行原 `/goal` 命令并 patch 原卡或发送文本结果 |
| `goal.pause` / `goal.resume` / `goal.clear` / `goal.edit` / `goal.replace.*` / `goal.edit.submit` | 调用 Codex `thread/goal/get|set|clear` | 先返回 toast，后台执行原 action 逻辑；若原结果是 card，则 patch 原卡，若原结果是 text/toast，则发送文本结果 |
| `workspace.clone.submit` | 后台 `git clone` | 回调只做 preflight 和状态切换 |
| `path_picker.confirm` `download_file` 分支 | 真实飞书 `ShareLocalFile` | 回调先返回，后台分享并 patch card |

已覆盖的关键 async guard:

- `internal/app/card_action_async_test.go` 覆盖 `upgrade.dev`、`menu.review.uncommitted`、`menu.review.base`、`review.base.select`、`review.form.submit`、`menu.plan`、`goal.*`、Claude `menu.interrupt`。
- `internal/app/compact_more_test.go` 覆盖 compact callback 先返回 preparing card。

### 轻路径/排除

| Action | 原因 |
| --- | --- |
| `menu.download` | 只打开路径选择器，不是真实飞书分享 |
| `menu.codex_upgrade` | 只做本地 probe，不查远端最新版本 |
| `codex_upgrade.refresh` | 只做本地 probe，不查远端最新版本 |
| `path_picker.confirm` 的 `workspace_new` / `workspace_clone` / `upgrade_local_binary` 分支 | 只做本地路径确认或本地文件逻辑 |

#### 已确认快路径，不再重复判断

| Action | 当前口径 |
| --- | --- |
| `menu.model` | 已确认是 Codex 本地快路径 |
| `model.config.set_model` / `model.config.select_model` | 已确认是 Codex 本地快路径 |
| `model.config.set_effort` / `model.config.select_effort` | 已确认是 Codex 本地快路径 |
| `menu.skills` / `skills.reload` / `skills.select` | 已确认是 Codex 本地快路径 |
| `menu.history` / `history.page` / `history.detail` / `history.detail.select` | 已确认是 Codex 本地快路径 |
| `menu.new` / `menu.fork` / `thread.resume.select` | 已确认是 Codex 本地快路径 |
| `workspace.use.select` / `workspace.use.existing` / `workspace.clone.use_existing` / `workspace.new.submit` | thread binding 已异步化，回调先返回 |

补充说明:

- 这条”已确认快路径”只适用于 `Codex` 本地路径。
- `workspace.*` 系列动作的 thread binding 当前保持异步化，适用于所有 backend。

## Claude

`Claude` 侧继承上面所有通用重流程项的异步保护要求:

- `review.*`
- `upgrade.dev`
- `workspace.clone.submit`
- `path_picker.confirm` 的 `download_file` 分支
- `menu.upgrade`
- `claude_upgrade.check/prepare` 这一类维护操作

除此以外，再单独看 Claude runtime / CLI。

### 同步风险

截至 2026-07-10，当前没有已知仍在 Claude 卡片 callback 同步 ack 路径执行长 runtime / CLI 往返的动作。

补充说明:

- `menu.interrupt` 最终仍会走 Claude runtime，再由活跃 CLI session 发送 interrupt control request；但当前已通过 `CompleteAsyncCommandAction` 先返回 preparing card。
- Codex backend 的 `menu.interrupt` 走本地 `turn/interrupt` 控制路径，仍按快路径处理。

### 重流程但已异步保护

| Action | 重流程来源 | 当前保护方式 |
| --- | --- | --- |
| `menu.interrupt` | Claude runtime `Interrupt()` / CLI control request | 先返回 preparing card，后台执行 `/stop` 并 patch |
| `menu.compact` | Claude 的 `/compact` 是长流程 | 回调先返回 preparing card，后台再入队提交给 Claude |
| `menu.claude_upgrade` | Claude 本机状态读取 | 先返回 preparing card，后台执行 |
| `claude_upgrade.check` | 探测 `claude update` 自升级命令 | 先返回 preparing card，后台执行 |
| `claude_upgrade.prepare` | 同上 | 先返回 preparing card，后台执行 |
| `workspace.use.select` | Claude 新工作区需启动 CLI 子进程 | `CompleteWorkspaceUse` 回调先返回，thread binding 异步执行 |
| `workspace.use.existing` | 同上 | 同上 |
| `workspace.clone.use_existing` | 同上 | 同上 |
| `workspace.new.submit` | `CreateWorkspaceAndSwitch` 中 thread binding | 回调先返回，thread binding 异步执行 |

补充说明:

- `Claude /compact` 业务上明确按长操作处理。
- 当前实现是正确方向: 不要把它重新拉回同步 callback 路径。

### 轻路径/排除

#### 1. 本轮明确排除的 Claude runtime / CLI 动作

| Action | 原因 |
| --- | --- |
| `menu.new` | 当前先不纳入 timeout 重点集合 |
| `thread.resume.select` | 当前先不纳入 timeout 重点集合 |
| `menu.fork` | 当前先不纳入 timeout 重点集合 |
| `model.config.set_model` / `model.config.select_model` | 当前先不纳入 timeout 重点集合 |
| `model.config.set_effort` / `model.config.select_effort` | 当前先不纳入 timeout 重点集合 |
| `thread.permission_mode.set` | 当前先不纳入 timeout 重点集合 |
| `workspace.permission_mode.set` | 当前先不纳入 timeout 重点集合 |

这批动作里有些确实会打 Claude runtime / CLI，但本轮收口口径里，不把它们当 timeout 重点集合。

#### 2. 轻控制路径

| Action | 为什么轻 |
| --- | --- |
| `approval.command.accept` | 只把审批结果投递回本地 pending interaction |
| `approval.command.accept_session` | 同上 |
| `approval.command.decline` | 同上 |
| `approval.command.cancel` | 同上 |
| `approval.file.accept` | 同上 |
| `approval.file.accept_session` | 同上 |
| `approval.file.decline` | 同上 |
| `approval.file.cancel` | 同上 |
| `approval.permissions.accept_turn` | 同上 |
| `approval.permissions.accept_session` | 同上 |
| `user_input.answer` | 只把答案投递回本地 pending interaction |
| `pending_form.cancel` | 对 Claude question / plan 这类 pending，本质也是本地取消投递 |

补充说明:

- 这些动作服务的是一个已经阻塞住的 Claude 会话，但同步 ack 路径本身通常只是本地 channel 投递。
- 因此当前不把它们放进 timeout 重点集合。

## 两条维护规则

### 1. 新动作默认先保守

如果一个新动作有明显的 `git`、网络、飞书透传、长 CLI / runtime 交互可能性，在还没有明确证据前，先按重路径设计:

- `fast callback ack`
- `async work`
- `card patch / follow-up`

### 2. 审计结论要跟实现细节一起维护

下面几种情况发生变化时，本文必须一起更新:

- 菜单桥接是否还在使用 `commandCaptureClient`
- 某个动作是否从同步执行改成了异步包装
- Claude 某类 control-response 是否从本地 pending 投递改成了显式 CLI 往返
- `review`、`upgrade`、`download`、`clone`、`compact`、`interrupt` 的执行边界发生变化
- `goal`、`plan` 这类 Codex RPC-backed card action 是否仍保持 fast toast ack + async patch/text 结果

## 最终收口

截至 2026-07-10，当前没有已知仍未异步保护的重型卡片 callback。

当前最需要持续 guard 的集合是“业务很重，但必须保持 fast ack -> async work -> patch”的动作:

- Codex / 通用:
  - `menu.review.uncommitted`
  - `menu.review.base`
  - `menu.review.commit`
  - `review.base.select`
  - `review.commit.select`
  - `review.form.submit` 的 `base` / `commit` 模式
  - `upgrade.dev`
  - `workspace.clone.submit`
  - `path_picker.confirm` 的 `download_file` 分支
  - `menu.upgrade`
  - `codex_upgrade.check`
  - `codex_upgrade.prepare`
  - `menu.plan`
  - `menu.goal`
  - `goal.*`
- Claude:
  - 上述通用项全部继承
  - `menu.interrupt`
  - `menu.compact`
  - `menu.claude_upgrade`
  - `claude_upgrade.check`
  - `claude_upgrade.prepare`

已确认按轻路径或本地控制路径处理的典型动作:

- Claude 的 `approval.*`
- `user_input.answer`
- `pending_form.cancel` 在 Claude question / plan 这类 pending 上
- 只做本地 probe 的 `menu.codex_upgrade` / `codex_upgrade.refresh` / `claude_upgrade.refresh`
- 用户已确认的 Codex 本地快路径:
  - `menu.model`
  - `model.config.*`
  - `menu.skills` / `skills.*`
  - `menu.history` / `history.*`
  - `menu.new` / `menu.fork` / `thread.resume.select`

如果后续需要自动化 guard，建议先从这份集合里挑最容易回归的动作做 callback-path 级测试。
