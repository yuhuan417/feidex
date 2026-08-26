# 使用指南

本文覆盖 Feidex 的会话语义、菜单与命令、Plan 模式、各类卡片交互与故障排查。配置项见 [配置参考](configuration.md)，运行与升级见 [运行与升级](operations.md)。

## 消息与会话语义

### Session

单聊和群聊统一使用同一种 session 身份：

- 以 `frontend_id + chat_id` 作为 session key
- 标准 SessionKey 为 `feishu:frontend:<frontend_id>:chat:<chat_id>`

也就是说，同一个 frontend 下的同一个 Feishu chat 会共享同一个 session；不同 frontend 的 bot 即使处理同一个 chat，也使用各自隔离的 session。`ChatType`、`UserID`、`RootMessageID`、`BindingID`、workspace 和本地 runtime 配置都只是 session/submission 元数据，不参与 session key。RootMessageID 继续用于回复树/turn 绑定元数据保存。

兼容旧状态时，历史 `group` / `p2p` / `root` / `user` 形式的 key 会被解析并归一化到当前 `frontend/chat` 形式。

### Queue 与 Steer

当前逻辑：

- 用户直接发新消息
  - 走 queue / 新 turn
- 用户回复某条消息
  - 先按该消息的 `rootId`/父消息找当前绑定的 turn
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

### Thread 恢复与 Workspace 选择

- 服务启动时，会尽量恢复 session 上次活动的 thread
- 切换 workspace 时，会优先恢复该 workspace 最近可用的 thread
- 如果当前 workspace 没有可恢复 thread，会立即创建一个新的 thread
- `/thread new` 会立刻创建并切换到新 thread
- `/thread fork` 会复制当前 thread 为分支线程，并立即切换过去

## 菜单与命令

主菜单按后端分不同入口（Codex 侧重 thread，Claude 侧重 session）：

- 当前 Bot
  - `当前工作区 /workspace`
  - `模型配置 /model`
  - `响应速度 /fast config`
  - 查看当前 bot 在本群内的 workspace、primary 和本地运行参数
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

群聊中的菜单和命令作用域规则：没有特殊 `@` 时由本群 primary bot 处理；明确 `@Bot` 时由被 `@` 的 bot 处理。群聊中的 `/workspace use`、`/workspace sandbox/policy/multiagent/permissions`、`/model set`、`/model effort`、`/effort`、`/fast` 以及对应菜单按钮，都会写入当前 bot 在当前群内的本地配置，不会切换无作用域的 session workspace 或全局模型配置。`@Bot /primary on` 会把被 `@` 的 bot 设为本群 primary owner；`/primary off` 不支持，切换 primary 要把另一个 bot 设为 owner。所有能收到这条群消息的 Feidex 实例都会同步本地 owner 副本。要操作另一个 bot，必须明确 `@Bot /menu` 或 `@Bot /workspace`，菜单里不提供 bot 选择列表或跨 bot handoff。

当前 bot 在群内还没有 workspace 时，不能执行普通输入。当前 bot 收到应处理的普通群消息时，会先暂存原始消息并展示当前工作区配置入口；用户完成 `/workspace use`、`/workspace new` 或 `/workspace clone` 后，原始消息会自动继续处理。

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
  - 在群聊中，`/model set` 和 `/model effort` 修改当前 bot 在当前群内的覆盖；`/model plan` 和 Claude 候选模型管理仍是 bot/frontend 默认配置，建议私聊 bot 配置
  - Claude 后端可在卡片中添加/移除候选 model；也可用 `/model option add <model-id>`、`/model option remove <model-id>`
- `/fast`
  - 配置当前 thread 的 service tier
  - 在群聊中，配置当前 bot 在当前群内的 service tier 覆盖
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
- `/goal`
  - 查看或创建当前 Codex thread 的长期任务目标
- `/goal <objective>`
  - 设置当前 Codex thread 的长期任务目标
- `/goal pause` / `/goal resume` / `/goal clear` / `/goal edit`
  - 暂停、恢复、清除或编辑当前 thread goal
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
  - 在群聊中，打开当前 bot 在当前群内的 workspace 选择/状态卡
- `/workspace list`
  - 打开工作区列表并可直接切换
  - 在群聊中，显示当前 bot 可选择的本机 workspace
- `/workspace new`
  - 新建工作区
  - 在群聊中，使用 `/workspace new WORKSPACE_ID CWD` 创建本机 workspace 并设为当前 bot 在本群的 workspace
- `/workspace use ID`
  - 切换工作区
  - 在群聊中，将当前 bot 在本群的 workspace 设为本机 workspace `ID`，不切换 session workspace
- `/workspace sandbox`
  - 配置 workspace 默认 sandbox
  - 在群聊中，使用 `/workspace sandbox MODE|default` 配置当前 bot 在本群的 sandbox 覆盖
- `/workspace policy`
  - 配置 workspace 默认 approval policy
  - 在群聊中，使用 `/workspace policy POLICY|default` 配置当前 bot 在本群的 approval policy 覆盖
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
  - 在群聊中，clone 后创建本机 workspace 并设为当前 bot 在本群的 workspace
- `/workspace choose`
  - 按钮式工作区选择器（按最近使用排序）
  - 在群聊中，作为当前 bot 在本群的 workspace 选择器
- `/workspace delete [ID]`
  - 删除工作区配置（不删磁盘文件）
  - 如果该 workspace 仍被当前 frontend 的群内配置引用，会拒绝删除；先在群里用 `/workspace use` 切到其他 workspace
- `/workspace permissions [MODE]`
  - 配置 workspace 默认 Claude 权限模式
  - 在群聊中，使用 `/workspace permissions MODE|default` 配置当前 bot 在本群的 Claude permission 覆盖

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

## Thread Goal（Codex only）

- `/goal` 作用在当前活动 Codex thread；如果当前没有已保存的 active thread，需要先发送一条普通消息或恢复一个 thread
- `/goal` 会读取当前 thread goal；没有 goal 时会打开 objective 输入卡
- `/goal <objective>` 会创建或更新 active goal；如果当前已有未完成 goal，会先要求确认替换
- `/goal pause`、`/goal resume`、`/goal clear`、`/goal edit` 分别用于暂停、恢复、清除和编辑当前 goal
- 命令行入口当前不解析 token budget 参数；编辑卡会保留已有 token budget
- goal 管理卡只是控制面。后端主动继续 active goal 时，Feidex 会为该 continuation turn 新发一张根卡作为回复锚点，不会把 turn 输出回复到 `/goal` 管理卡上

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
