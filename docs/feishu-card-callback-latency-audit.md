# 飞书卡片回调延迟审计

> 更新时间: 2026-04-23
>
> 目的:
> - 收口当前 Feidex 的飞书卡片回调延迟风险集合。
> - 给后续新增卡片动作提供统一判断标准。
> - 区分“同步 ack 路径风险”和“业务上是重流程，但当前已异步保护”。

主要代码来源:

- `internal/app/actions.go`
- `internal/app/menu_command_bridge.go`
- `internal/app/action_registry_menu.go`
- `internal/app/action_registry_workspace.go`
- `internal/app/action_registry_maintenance.go`
- `internal/app/action_registry_pending.go`
- `internal/app/review.go`
- `internal/app/review_forms.go`
- `internal/app/review_git.go`
- `internal/app/download.go`
- `internal/app/path_picker_actions.go`
- `internal/app/upgrade.go`
- `internal/app/codex_upgrade.go`
- `internal/app/claude_upgrade.go`
- `internal/app/compact.go`
- `internal/app/thread_menu.go`
- `internal/app/workspace_feature_actions.go`
- `internal/app/workspace_creation_clone.go`
- `internal/app/claude_runtime.go`
- `internal/app/backend_server_request_adapter.go`
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

| Action | 风险来源 | 当前结论 |
| --- | --- | --- |
| `menu.review.uncommitted` | 同步 `git rev-parse` + `git status` | 保留在风险集合 |
| `menu.review.base` | 同步列 branch，走 `git for-each-ref` | 保留在风险集合 |
| `menu.review.commit` | 同步列 commit，走 `git log` | 保留在风险集合 |
| `review.base.select` | 重新列 branch | 保留在风险集合 |
| `review.commit.select` | 可能重新列 commit 以补 title | 保留在风险集合 |
| `review.form.submit` | `base` 模式会跑 `rev-parse/status/diff`；`commit` 模式会跑 `rev-parse/log -1` | 保留在风险集合 |
| `upgrade.dev` | 同步查远端 release | 明确按长操作处理 |

补充说明:

- `review.form.submit` 的 `custom` 模式不碰 `git`，不在这条风险里。
- 这组 `review.*` 不一定每次都慢，但明显受仓库规模和本地磁盘状态影响，因此继续保守纳入风险集合。

### 重流程但已异步保护

| Action | 重流程来源 | 当前保护方式 |
| --- | --- | --- |
| `menu.upgrade` | 查远端 release | 先返回 preparing card，后台执行 |
| `codex_upgrade.check` | `npm view @openai/codex version --json` | 先返回 preparing card，后台执行 |
| `codex_upgrade.prepare` | 同上 | 先返回 preparing card，后台执行 |
| `workspace.clone.submit` | 后台 `git clone` | 回调只做 preflight 和状态切换 |
| `path_picker.confirm` `download_file` 分支 | 真实飞书 `ShareLocalFile` | 回调先返回，后台分享并 patch card |

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
| `workspace.use.select` / `workspace.use.existing` / `workspace.clone.use_existing` / `workspace.new.submit` | 已确认是 Codex 本地快路径 |

补充说明:

- 这条“已确认快路径”只适用于 `Codex` 本地路径。
- `Claude` 会话管理类动作是否要异步化，仍按它自己的 runtime / CLI 边界单独判断。

## Claude

`Claude` 侧继承上面所有通用风险项:

- `review.*`
- `upgrade.dev`
- `workspace.clone.submit`
- `path_picker.confirm` 的 `download_file` 分支
- `menu.upgrade`
- `claude_upgrade.check/prepare` 这一类维护操作

除此以外，再单独看 Claude runtime / CLI。

### 同步风险

| Action | 风险来源 | 当前结论 |
| --- | --- | --- |
| `menu.interrupt` | 会同步调用 Claude runtime `Interrupt()` | 当前按“可能长”保守纳入风险集合 |

补充说明:

- `menu.interrupt` 最终会走 Claude runtime，再由活跃 CLI session 发送 interrupt control request。
- 它不属于“明确的后台异步包装动作”，因此先按风险集合保守收口。

### 重流程但已异步保护

| Action | 重流程来源 | 当前保护方式 |
| --- | --- | --- |
| `menu.compact` | Claude 的 `/compact` 是长流程 | 回调先返回 preparing card，后台再入队提交给 Claude |
| `menu.claude_upgrade` | Claude 本机状态读取 | 先返回 preparing card，后台执行 |
| `claude_upgrade.check` | `npm view @anthropic-ai/claude-code version --json` | 先返回 preparing card，后台执行 |
| `claude_upgrade.prepare` | 同上 | 先返回 preparing card，后台执行 |

补充说明:

- `Claude /compact` 业务上明确按长操作处理。
- 当前实现是正确方向: 不要把它重新拉回同步 callback 路径。

### 轻路径/排除

#### 1. 本轮明确排除的 Claude runtime / CLI 动作

| Action | 原因 |
| --- | --- |
| `menu.new` | 当前先不纳入 timeout 重点集合 |
| `workspace.use.select` | 当前先不纳入 timeout 重点集合 |
| `workspace.use.existing` | 当前先不纳入 timeout 重点集合 |
| `workspace.clone.use_existing` | 当前先不纳入 timeout 重点集合 |
| `workspace.new.submit` | 当前先不纳入 timeout 重点集合 |
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

## 最终收口

截至 2026-04-23，当前最需要持续盯住的飞书卡片回调 timeout 集合是:

- Codex:
  - `menu.review.uncommitted`
  - `menu.review.base`
  - `menu.review.commit`
  - `review.base.select`
  - `review.commit.select`
  - `review.form.submit` 的 `base` / `commit` 模式
  - `upgrade.dev`
- Claude:
  - 上述通用项全部继承
  - `menu.interrupt`
  - `menu.compact` 作为长流程必须保持异步保护

已确认可从 timeout 重点集合排除的典型路径:

- Claude 的 `approval.*`
- `user_input.answer`
- `pending_form.cancel` 在 Claude question / plan 这类 pending 上
- 只做本地 probe 的 `menu.codex_upgrade` / `codex_upgrade.refresh` / `menu.claude_upgrade` / `claude_upgrade.refresh`
- 用户已确认的 Codex 本地快路径:
  - `menu.model`
  - `model.config.*`
  - `menu.skills` / `skills.*`
  - `menu.history` / `history.*`
  - `menu.new` / `menu.fork` / `thread.resume.select`
  - `workspace.use.select` / `workspace.use.existing` / `workspace.clone.use_existing` / `workspace.new.submit`

如果后续需要自动化 guard，建议先从这份集合里挑最容易回归的动作做 callback-path 级测试。
