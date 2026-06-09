# 使用指南

本文覆盖 Feidex 的会话语义、菜单与命令、Plan 模式、各类卡片交互与故障排查。配置项见 [配置参考](configuration.md)，运行与升级见 [运行与升级](operations.md)。

## 消息与会话语义

### Session

单聊：

- 以 `chat_id + user_id` 作为 session key

群聊：

- 以 `chat_id + root_message_id` 作为 session key

也就是说，群聊中同一个根消息树会共享同一个 session。

### Queue 与 Steer

当前逻辑：

- 用户直接发新消息
  - 走 queue / 新 turn
- 用户回复某条消息
  - 先按该消息的 `rootId` 找当前绑定的 turn
  - 能 steer 就 steer
  - steer 失败自动回退 queue

### Staged Attachments

用户先发图片、文件或其他支持的附件、后发文字时：

- 附件先进入暂存桶
- 下一条文字会把这些暂存附件一起带入输入
- 形成新 turn 成功后，会把所有参与本次输入的 root 绑定到该 turn

### `/history`

`/history` 不是从本地 submission 拼出来的，而是直接调用 `thread/read(includeTurns=true)` 读取 thread 历史，因此：

- 同一个 thread 上来自 Codex CLI / VSCode / app-server 的 turn 都能看见
- 当前展示重点是每个 turn 的输入和状态
- 历史输入摘要当前会渲染 `userMessage.content` 里的 `text`、`image`、`localImage`、`skill`、`mention`
- `skill` 会显示为 `[skill] <name>`；如果没有 `name`，会回退显示 `path`

### Thread 恢复与 Workspace 绑定

- 服务启动时，会尽量恢复 session 上次活动的 thread
- 切换 workspace 时，会优先恢复该 workspace 最近可用的 thread
- 如果当前 workspace 没有可恢复 thread，会立即创建一个新的 thread
- `/thread new` 会立刻创建并切换到新 thread
- `/thread fork` 会复制当前 thread 为分支线程，并立即切换过去

## 菜单与命令

主菜单按后端分不同入口（Codex 侧重 thread，Claude 侧重 session）：

- 常用工具
  - `中断任务 /stop`
  - `静默模式 /quiet`
  - `压缩上下文 /compact`
  - `下载文件 /download`
  - `历史记录 /history`
  - `Token 消耗 /usage`
  - `代码审查 /review`（Codex only）
- model
  - `模型配置 /model`
  - `推理强度 /effort`（Claude only）
  - `响应速度 /fast`
- thread（Codex）/ session（Claude）
  - list 下拉切换
  - 新建 / fork / resume
  - 配置 sandbox / policy / permissions
- workspace
  - `list` 下拉切换工作区
  - `新建工作区 /workspace new`
  - `克隆工作区 /workspace clone`
  - `选择工作区 /workspace choose`
  - `删除工作区 /workspace delete`
  - `配置默认沙箱 /workspace sandbox`
  - `配置默认策略 /workspace policy`
  - `配置默认权限模式 /workspace permissions`（Claude）
- system
  - `后端管理 /backend`
  - `日志级别 /debug`
  - `查看日志 /debug logs`
  - `升级服务 /upgrade`
  - `Codex CLI /codex`
  - `Claude CLI /claude`
  - `状态面板 /status`
  - `命令帮助 /help`

### 本地 slash 命令

- `/menu`
  - 打开主菜单
- `/help`
  - 查看命令说明
- `/interrupt` 或 `/stop`
  - 中断当前任务
- `/quiet`
  - 切换 Quiet 模式
- `/quiet on`
  - 开启 Quiet 模式
- `/quiet off`
  - 关闭 Quiet 模式
- `/compact`
  - 压缩当前线程上下文
- `/download`
  - 在当前 workspace 范围内选择文件并生成下载链接
- `/history`
  - 查看当前 thread 的历史记录
- `/usage`
  - 查看当前 thread 的累计 token usage
- `/model`
  - 打开模型选择与推理强度配置
- `/fast`
  - 配置当前 thread 的 service tier
- `/debug`
  - 切换服务端 slog 日志级别（debug/info）
  - 仅 `debug_allow_from` 白名单内用户可用
- `/debug on`
  - 切换到 debug 级别
- `/debug off`
  - 切换到 info 级别
- `/debug logs`
  - 查看最近一段服务端 slog 日志
  - 仅 `debug_allow_from` 白名单内用户可用
- `/plan`
  - 切换当前 Codex thread 的 `plan` collaboration mode
- `/plan on`
  - 为当前 Codex thread 开启 `plan` collaboration mode
- `/plan off`
  - 关闭当前 Codex thread 的 `plan` collaboration mode
- `/thread`
  - 打开 thread 菜单
- `/thread list`
  - 查看当前工作区可恢复的线程
- `/thread list all`
  - 查看更多来源的线程
- `/thread new`
  - 立即创建并切换到新的 thread
- `/thread fork`
  - fork 当前线程并切换到新的分支线程
- `/thread sandbox`
  - 配置当前 thread 的 sandbox
- `/thread policy`
  - 配置当前 thread 的 approval policy
- `/threads`
  - 等价于 `/thread list`
- `/new`
  - 等价于 `/thread new`
- `/fork`
  - 等价于 `/thread fork`
- `/workspace`
  - 打开工作区菜单
- `/workspace list`
  - 打开工作区列表并可直接切换
- `/workspace new`
  - 新建工作区
- `/workspace use ID`
  - 切换工作区
- `/workspace sandbox`
  - 配置 workspace 默认 sandbox
- `/workspace policy`
  - 配置 workspace 默认 approval policy
- `/status`
  - 查看当前状态
- `/upgrade`
  - 检查最新版本并发起升级
- `/upgrade dev`
  - 查询 `dev-latest` prerelease，并升级到当前最新开发版构建
- `/upgrade v0.3.0`
  - 跳过最新版本探测，直接发起指定版本升级确认
- `/upgrade local`
  - 打开当前 workspace 的文件选择器，选择本地 Binary 升级
- `/upgrade path ./dist/feidex-linux-amd64`
  - 直接用当前 workspace 下的本地 Binary 发起升级确认
- `/backend`
  - 查看可用后端，空闲时切换 Codex ↔ Claude
- `/backend retry on` / `/backend retry off`
  - 开关自动 retry 失败线程
- `/codex`
  - 检查 Codex CLI 安装/升级状态
- `/codex check`
  - 检查 Codex CLI 自升级命令是否可用
- `/codex upgrade`
  - 通过 Codex CLI 自带升级命令更新，并做 runtime smoke test
- `/codex restart`
  - 重启 Codex 运行时（空闲时）
- `/claude`
  - 检查 Claude CLI 安装/升级状态
- `/claude check`
  - 检查 Claude CLI 自升级命令是否可用
- `/claude upgrade`
  - 通过 Claude CLI 自带升级命令更新，并做 runtime smoke test
- `/claude restart`
  - 重启 Claude 运行时（空闲时）
- `/session`
  - Claude session 菜单（对应 Codex 的 `/thread`）
- `/session list` / `/session list all`
  - 查看可恢复的 Claude session
- `/session new`
  - 新建 Claude session
- `/session fork`
  - fork 当前 Claude session
- `/session resume SESSION_ID`
  - 恢复指定 Claude session
- `/session permissions [MODE]`
  - 配置 Claude session 权限模式
- `/effort [effort]`
  - 设置 Claude reasoning effort（`low`/`medium`/`high`/`xhigh`/`max`）
- `/review`
  - Review 未提交变更（Codex only）
- `/review uncommitted`
  - 等价于 `/review`
- `/review base [branch]`
  - Review 相对指定 base 分支的变更
- `/review commit [rev]`
  - Review 指定 commit
- `/review custom [instructions]`
  - 自定义 review 指令
- `/skills`
  - 查看可用 Codex 技能
- `/skills reload`
  - 刷新技能列表
- `/workspace clone GIT_URL [ID] [--parent DIR]`
  - clone Git 仓库创建新工作区
- `/workspace choose`
  - 按钮式工作区选择器（按最近使用排序）
- `/workspace delete [ID]`
  - 删除工作区配置（不删磁盘文件）
- `/workspace permissions [MODE]`
  - 配置 workspace 默认 Claude 权限模式

## Plan Mode（Codex only）

- `/plan` 作用在当前活动 thread，需要 `[codex].experimental_api = true`
- 开启后，当前 thread 会切到 Codex 的 `plan` collaboration mode；session-scoped 内容卡标题会带 `[plan]` 前缀，便于和普通执行态区分
- Plan 模式的模型和推理强度可单独通过 `/model` 配置：
  - `/model plan`
  - `/model plan set <model-id|default>`
  - `/model plan effort <effort|default>`
- `plan_model` 留空时跟随 default mode 的模型；`plan_reasoning_effort` 留空时跟随 plan preset
- 当真正的 plan turn 完成并产出正式计划结果后，Feidex 会发送 `Implement this plan?` 确认卡，而不是直接退出 plan mode
- 退出确认有三个动作：
  - `Yes, implement this plan`
    - 退出当前 thread 的 plan mode，并在当前 thread 直接提交实现指令
  - `Yes, clear context and implement`
    - 新开一个 fresh thread，把刚生成的计划当成实现输入继续执行
  - `No, stay in Plan mode`
    - 保持当前 thread 继续处于 plan mode
- `/plan off` 或再次切换 `/plan` 时，会清掉当前 thread 的 plan mode；旧的计划确认卡会随之失效

## 审批卡片

支持三类审批：

- 命令审批
- 文件变更审批
- 权限审批

审批处理后，卡片不会只剩“已允许本会话执行”这类结果文案，而会保留原审批内容，便于回看上下文。

## 补充输入卡片

- `request_user_input`
  - 单题、`1-3` 个选项、非多选、非 `other` 时走 quick-card 按钮
  - 多题、多选、自由文本或允许 `other` 的场景走表单卡片
- quick-card 会在正文里重复展示完整题目、题目 ID 和全部选项说明
  - 如果 option 同时带 `label` 和 `description`，正文和提交后的确认卡片都会显示成 `label - description`
  - 这样在手机端即使按钮本身显示不全，用户仍能从正文确认自己在选什么
- 提交后，确认卡片会保留完整已选项文案，而不是只回显短 label

## 文件下载与预览

- `/download` 会打开一个 workspace 范围内的文件选择器
- 路径选择器只允许浏览当前 workspace 根目录之内的路径
- 确认后会通过飞书云盘中转生成下载链接
- markdown 预览与本地文件分享共用同一套 artifact 流程
- final message 里引用的 workspace 本地文件会异步补成飞书云盘链接，不再只限 `.md`

## 诊断与可观测性

- `/usage`
  - 查看当前 thread 的累计 token usage，包括 input、cached input、output 和 reasoning output
- turn 通知和最终输出
  - 会附带本次 token usage、耗时，以及可用时的 `context left`
- `/debug`、`/debug on`、`/debug off`
  - 运行时切换服务端 slog 日志级别
- `/debug logs`
  - 查看内存缓冲中的最近服务端日志
- 飞书权限问题
  - 如果接口调用因权限或鉴权问题失败，会优先渲染诊断卡片，附带 `log_id`、帮助链接和更具体的失败原因

## 故障排查

### 1. 群里发消息没反应

优先检查：

- `group_at_only = true` 时是否真的 `@bot`
- `allow_from` 是否限制了发送者
- 飞书应用权限是否配置正确

### 2. `/upgrade` 提示当前环境不支持

说明当前平台不支持自动升级，或者当前进程不是 daemon 服务进程。

### 3. 回复消息没有 steer

当前逻辑是：

- 只有“回复消息”才尝试 steer
- 如果 root 对应 turn 不可 steer，会自动回退 queue

所以看起来像“没 steer”，有可能其实是已经自动回退为新 turn。

### 4. 线程历史为空

`/history` 依赖 `thread/read(includeTurns=true)`。

如果当前 thread 本身没有 turn，或者 thread 还没正确加载，就可能看不到历史。
