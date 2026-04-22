# Claude Backend Menu / Slash Command 能力审计

> 更新时间: 2026-04-21
>
> 审计口径:
> - 只看“当前 frontend 已选择 `claude` backend，且不在维护模式”时的正常行为。
> - 不把维护模式下的临时封禁混进能力矩阵。维护模式是另外一层限制。
>
> 代码来源:
> - `internal/app/commands.go`
> - `internal/app/command_registry.go`
> - `internal/app/claude_support.go`
> - `internal/app/thread_menu.go`
> - `internal/app/menu_specs.go`
> - `internal/app/menu_actions.go`
> - `internal/app/history.go`
> - `internal/app/skills.go`
> - `internal/app/model_config.go`
> - `internal/app/service_tier.go`
> - `internal/app/review.go`
> - `internal/app/compact.go`
> - `internal/app/status_panel.go`
> - `internal/app/claude_runtime.go`

## 结论速览

- Claude 当前真正不支持的不是“少量命令”，而是一整组 Codex 绑定能力: `/history`、`/skills`、`/model`、`/fast`、`/review`、`/compact`、`/fork`、`/threads`，以及 `/thread` 的大部分子命令。
- 现状最大的 UX 问题不是报错本身，而是菜单和 `/help` 没有按 backend 过滤。Claude 用户现在能看到很多 Codex-only 入口。
- `/thread` 在 Claude 上是“部分支持”，不是完全不支持。当前只支持继续当前会话和 `/thread new`。
- `/skills` 当前是“实现路径耦合到 Codex”，不是“理论上 Claude 永远做不了”。`buildClaudePrompt()` 已经会把 `Submission.Skills` 注入 Claude prompt。
- `/history`、`/compact`、`/review` 则是实打实依赖 Codex RPC，短期内不能靠 UI 小修补解决。

## Slash Command

### Claude 上可用

| 命令 | 状态 | 说明 |
| --- | --- | --- |
| `/menu` | ✅ | 可用。打开命令菜单。 |
| `/backend` | ✅ | 可用。查看或切换 backend。 |
| `/usage` | ✅ | 可用。查看当前线程 token usage。 |
| `/quiet` | ✅ | 可用。Quiet mode 配置是本地能力。 |
| `/debug` | ✅ | 可用。服务端日志级别与日志查看是本地能力。 |
| `/download` | ✅ | 可用。工作区文件下载是 Feidex 本地能力。 |
| `/interrupt` / `/stop` | ✅ | 可用。中断当前运行任务。 |
| `/new` | ✅ | 可用。等价于 `/thread new`。 |
| `/thread new` | ✅ | 可用。为当前 Claude frontend 新建会话。 |
| `/workspace` 及其子命令 | ✅ | 可用。工作区管理没有被 Claude blocklist 拦截。 |
| `/status` | ⚠️ | 可用，但卡片里有些字段仍是 Codex 口径。 |
| `/help` | ⚠️ | 可用，但帮助内容没有按 backend 过滤。 |
| `/claude` | ✅ | 可用。Claude CLI 安装、检查、升级、重启。 |
| `/codex` | ✅ | 可用。即使当前 backend 是 Claude，也允许管理本机 Codex CLI。 |
| `/upgrade` | ✅ | 可用。Feidex 自身升级，与当前 backend 无关。 |
| `/thread` | ⚠️ | 可打开 Claude 专用线程卡，但只支持极少数动作。 |

### Claude 上部分支持

| 命令 | 当前行为 | 代码依据 |
| --- | --- | --- |
| `/thread` | 无参数时可打开 Claude 专用线程卡。卡片文案明确说明只支持继续当前会话与新建会话。 | `thread_menu.go`, `claude_support.go` |
| `/thread new` | 正常可用。 | `thread_menu.go` |
| `/help` | 会列出全部本地命令，包括 Claude 实际不支持的命令。 | `command_registry.go` 的 `renderHelpBodyFromRegistry()` |
| `/status` | 会显示 `全局模型`、`全局推理强度`、`thread service tier` 等 Codex 风格字段，Claude 下容易误导。 | `status_panel.go` |

### Claude 上不支持

| 命令 | 当前状态 | 原因 |
| --- | --- | --- |
| `/history` | ❌ | `commandHistory()` 依赖 Codex `thread/read`。 |
| `/history detail TURN_NUMBER` | ❌ | 同上。 |
| `/skills` | ❌ | 入口被 `claudeUnsupportedCommand()` 拦截；实现依赖 Codex `skills/list`。 |
| `/skills reload` | ❌ | 同上。 |
| `/model` | ❌ | 入口被拦截；实现依赖 Codex `model/list` 和 `config.Codex`。 |
| `/model set ...` | ❌ | 同上。 |
| `/model effort ...` | ❌ | 同上。 |
| `/fast` | ❌ | 入口被拦截；当前语义是 Codex `serviceTier`。 |
| `/fast fast/default/toggle/config` | ❌ | 同上。 |
| `/review` | ❌ | 入口被拦截；主流程依赖 Codex `review/start`。 |
| `/review ...` 所有子命令 | ❌ | 同上。 |
| `/compact` | ❌ | 入口被拦截；实现依赖 Codex `thread/compact/start`。 |
| `/fork` | ❌ | 入口被拦截；等价于 `/thread fork`。 |
| `/threads` | ❌ | 实际会转发到 `/thread list`，而 Claude 不支持 `/thread list`。 |
| `/thread list` | ❌ | Claude 线程菜单不支持列出可恢复 thread。 |
| `/thread list all` | ❌ | 同上。 |
| `/thread fork` | ❌ | Claude 线程菜单不支持 fork。 |
| `/thread resume THREAD_ID` | ❌ | Claude 线程菜单不支持按 thread id 恢复。 |
| `/thread sandbox [MODE]` | ❌ | Claude 线程菜单不支持 thread 级 sandbox。 |
| `/thread policy [POLICY]` | ❌ | Claude 线程菜单不支持 thread 级 approval policy。 |

### 与 skills 相关的额外说明

- 当前文档不能把 Claude 简化成“完全没有 skills 能力”。
- `buildClaudePrompt()` 已经会把 `Submission.Skills` 写进 Claude prompt。
- 但用户侧的 skills 选择链路目前仍然走 `skills/list`，这是 Codex RPC 能力。
- 因此 `$skill-name ...` 这条前缀路径在 Claude 上当前也没有真正打通，`resolveSubmissionSkill()` 仍会回到 `fetchSkillsForWorkspaceID()`。
- 因此现状更准确的说法是:
  - Claude 当前没有可用的 skills 发现/选择 UI。
  - 不是 Claude prompt 注入层做不到。

## 菜单

### 根因

- 菜单渲染基本不区分 backend。
- 大部分按钮只是把一个 slash command 回填给命令分发层。
- 真正的拦截点通常发生在 `handleCommand()` 之后的 Claude blocklist。
- 所以 Claude 用户今天会看到三种情况:
  - 正常可用的按钮。
  - 点一下立刻报“仅支持 Codex backend”的按钮。
  - 能进入子菜单，但子菜单里的实际动作又失败的“死胡同”。

### 根菜单与分组菜单

| 菜单节点 | Claude 状态 | 当前行为 |
| --- | --- | --- |
| `menu.root` | ⚠️ | 根菜单不裁剪，仍显示 `常用工具`、`模型配置`、`线程管理`、`工作区管理`、`系统运维` 五组。 |
| `menu.tools` | ⚠️ | 组本身能打开，但内部同时包含可用和不可用项。 |
| `menu.group.model` | ⚠️ | 组本身能打开，但里面两个入口都不可用。 |
| `menu.thread` | ⚠️ | 会切换到 Claude 专用线程卡，只支持继续当前会话和新建会话。 |
| `menu.workspace` | ✅ | 当前是可用的。 |
| `menu.group.system` | ✅ | 大部分可用，但其中的 `menu.help`、`menu.status` 内容不够 Claude-aware。 |

### 常用工具菜单

| Action | 标题 | Claude 状态 | 当前行为 |
| --- | --- | --- | --- |
| `menu.interrupt` | 中断任务 | ✅ | 直接走 `/stop`。 |
| `menu.review` | 代码审查 | ⚠️ | 不是立刻报错，而是先打开 review 子菜单卡；子菜单里的实际动作再失败。 |
| `menu.quiet` | 静默模式 | ✅ | 正常。 |
| `menu.compact` | 压缩上下文 | ❌ | 点击后直接走 `/compact`，马上失败。 |
| `menu.download` | 下载文件 | ✅ | 正常。 |
| `menu.history` | 历史记录 | ❌ | 点击后直接走 `/history`，马上失败。 |
| `menu.skills` | 技能列表 | ❌ | 点击后直接走 `/skills`，马上失败。 |
| `menu.usage` | Token 消耗 | ✅ | 正常。 |

### 模型配置菜单

| Action | 标题 | Claude 状态 | 当前行为 |
| --- | --- | --- | --- |
| `menu.group.model` | 模型配置 | ⚠️ | 会打开一个通用 model 分组卡。 |
| `menu.model` | 模型配置 | ❌ | 点击后走 `/model`，失败。 |
| `menu.fast` | 响应速度 | ❌ | 点击后走 `/fast config`，失败。 |

`menu.group.model` 目前是典型的误导入口: 根卡能开，子项全死。

### 线程管理菜单

| Action | 标题 | Claude 状态 | 当前行为 |
| --- | --- | --- | --- |
| `menu.thread` | 线程管理 | ⚠️ | 打开 Claude 专用线程卡。 |
| `menu.new` | 新建线程 | ✅ | 正常。 |
| `menu.fork` | 派生线程 | ❌ | Claude 不支持。 |
| `thread.resume.select` | 下拉恢复线程 | ❌ | Claude 卡片里不会提供这个选择器。 |
| `thread.sandbox.menu` | 配置线程沙箱 | ❌ | Claude 不支持。 |
| `thread.policy.menu` | 配置审批策略 | ❌ | Claude 不支持。 |

### 系统运维菜单

| Action | 标题 | Claude 状态 | 当前行为 |
| --- | --- | --- | --- |
| `menu.debug` | 日志级别 | ✅ | 正常。 |
| `menu.debug.logs` | 查看日志 | ✅ | 正常。 |
| `menu.backend` | Backend | ✅ | 正常。 |
| `menu.codex_upgrade` | Codex 管理 | ✅ | 正常。即使当前 backend 是 Claude 也允许。 |
| `menu.claude_upgrade` | Claude 管理 | ✅ | 正常。 |
| `menu.upgrade` | 升级服务 | ✅ | 正常。 |
| `menu.status` | 状态面板 | ⚠️ | 卡片可打开，但字段口径偏 Codex。 |
| `menu.help` | 命令帮助 | ⚠️ | 卡片可打开，但会展示 Claude 不支持的命令。 |

### 代码层面最误导的两个菜单

| 菜单 | 问题 | 直接原因 |
| --- | --- | --- |
| `menu.review` | Claude 用户能打开 review 菜单，但一旦点具体动作就失败。 | `completeMenuReview()` 直接渲染 review 卡，没有先走 backend 判断。 |
| `menu.group.model` | Claude 用户能打开模型分组卡，但 `menu.model` 和 `menu.fast` 都失败。 | `completeMenuGroupModel()` 直接渲染 model 组卡，没有先走 backend 判断。 |

## 为什么会这样

### 1. Claude blocklist 是按命令拦，不按菜单拦

- `handleCommand()` 在 backend 为 `claude` 时，会先调用 `claudeUnsupportedCommand()`。
- `claudeUnsupportedCommand()` 目前直接拦截:
  - `/history`
  - `/skills`
  - `/model`
  - `/review`
  - `/compact`
  - `/fork`
  - `/fast`
- 对 `/thread` 则额外只放行空命令和 `/thread new`。

这意味着“按钮可见”不等于“按钮可用”。

### 2. `/threads` 不是显式 blocklist，但实际上也不可用

- `/threads` 的 handler 会直接转发成 `/thread list`。
- 而 Claude 又不支持 `/thread list`。
- 所以它属于“间接不支持”。

### 3. 帮助和菜单当前都没有 capability filter

- `renderHelpBodyFromRegistry()` 会把全部本地命令都列出来。
- `menu_specs.go` 里的菜单定义也是全量静态注册。
- 所以 Claude 当前看到的是“产品总菜单”，不是“Claude 可用菜单”。

### 4. 一部分能力确实是 Codex RPC 绑定

| 能力 | 当前绑定点 |
| --- | --- |
| history | `thread/read` |
| compact | `thread/compact/start` |
| review | `review/start` |
| model list / reasoning effort | `model/list` + `config.Codex` |
| skills list | `skills/list` |

这几类能力不是只改前端卡片就能补齐的。

### 5. `/fast` 看起来像本地状态，实际语义仍然是 Codex service tier

- `setThreadServiceTier()` 只是先把值存进 session。
- 这个值后续会被带进 Codex 的 `thread/start`、`turn/start`、`fork` 参数。
- Claude 路径没有对应的 backend 语义。

因此把 `/fast` 继续暴露给 Claude 会让含义变得不明确。

## Claude CLI Protocol 给出的实现信号

上一节解释的是“为什么现在会失败”，但只停在 blocklist 不够。对照 `claude-cli-protocol/` 之后，更准确的判断应该是: 一部分能力是 Feidex 没接，不是 Claude CLI 没有。

### 1. `/thread resume` 与 `/threads` 缺的主要不是协议，而是索引层

`claude-cli-protocol` 里已经明确有这些信号:

- init `SystemMessage` 会返回 `session_id`
- CLI 启动参数支持 `--resume <session_id>`
- 上游 Go SDK 有 `WithResume(...)`
- 上游集成测试验证了 resume 后上下文会延续

所以:

- Claude 的“恢复会话”不是后端空白能力
- 当前真正缺的是 Feidex 自己维护的 session catalog

建议:

- 先把 Claude UI 文案从 “thread” 改成 “session”，避免和 Codex thread 语义混淆。
- 每次 Claude init 时持久化:
  - `frontend_id`
  - `workspace_id`
  - `session_id`
  - `started_at` / `updated_at`
  - 最近一条用户输入摘要
- 再用这份索引补 `/threads` 与 `/thread resume SESSION_ID`。

### 2. `/thread fork` 有协议线索，但没有现成 Feidex 实现

`claude-cli-protocol` 的文档和 `protocol.proto` 都出现了 `fork_session` / `--fork-session`。

这说明:

- Claude CLI 很可能支持“从当前上下文派生一个新 session”
- 但当前 Feidex 本地 wrapper `internal/claudecli` 没有把它暴露出来
- 仓库里也没有 live probe 证明它和 Feidex 预期的 fork 语义完全一致

建议:

- 先做 live probe，确认 `--fork-session` 的真实行为和返回的 session lineage。
- 确认稳定后，再给 `internal/claudecli` 增加 `WithForkSession`。
- 在验证之前，不建议直接把 Claude `/thread fork` 暴露给用户。

### 3. `/thread policy` 其实最接近现成能力

协议侧已经有:

- 启动参数 `--permission-mode`
- 运行中 control request `set_permission_mode`

上游 Go SDK 还直接实现了 `SetPermissionMode(...)`。

这意味着:

- Claude policy 不是“协议不支持”
- 当前只是 Feidex 本地 `internal/claudecli` 没把动态 `set_permission_mode` 接出来
- Feidex 现有 Claude 路径只在 `EnsureSession()` 启动时根据 config/workspace 算一次 permission mode

建议:

- 优先补 `internal/claudecli.SetPermissionMode(ctx, mode)`。
- 再把 `sess.ActiveThreadApprovalPolicy` 真正映射到 Claude session 的 permission mode，而不是只写本地 session 状态。
- Claude 菜单文案不要继续伪装成 Codex 的 `on-request` / `never`，应改成 Claude 的真实枚举:
  - `default`
  - `acceptEdits`
  - `plan`
  - `bypassPermissions`

### 4. `/thread sandbox` 不能照抄 Codex，但协议并不是完全没有支撑

`claude-cli-protocol` 的 `ClaudeAgentOptions` 里有 `sandbox`，CLI 通过 `--settings` 传入，文档里还定义了 `SandboxSettings`、`SandboxNetworkConfig`。

但官方注释也写得很清楚:

- 文件读限制靠 permission rules
- 文件写限制靠 Edit allow/deny
- 网络限制靠 WebFetch allow/deny

所以:

- Claude sandbox 不是 Codex `read-only / workspace-write / danger-full-access` 的一比一等价物
- 现有 `/thread sandbox` 菜单不能直接复用 Codex 语义

建议:

- 不要直接解封现有 Claude `/thread sandbox`
- 如果要做，应该重做一个 Claude-specific sandbox 配置卡
- 或者继续隐藏，避免制造“名字一样、语义不同”的伪兼容

### 5. `/model` 对 Claude 来说其实是可实现的

协议侧已经有这些原语:

- CLI 启动参数 `--model`
- 配置层字段 `fallback_model`
- 上游 Go SDK 已实现运行中 control request `set_model`

所以更准确的结论是:

- Claude `/model` 不是“后端没有”
- 当前只是 Feidex 的实现还绑在 `config.Codex` 和 Codex `model/list`

建议:

- 给 `internal/claudecli` 增加 `SetModel(ctx, model)`。
- 配置层增加 `claude.fallback_model`，不要只保留 `claude.model`。
- 给 Claude 单独做 model 配置卡，不要继续复用 Codex 的 reasoning-effort UI。

### 6. `/compact` 与 `/review` 不应该再简单归类成“Claude 不支持”

从协议 trace 能明确看到:

- init `SystemMessage` 会返回 `slash_commands`
- Claude CLI 当前广告的 slash commands 包括:
  - `compact`
  - `context`
  - `cost`
  - `review`
  - `security-review`
  - `pr-comments`
  - `release-notes`
- 协议里还有 `PreCompact` hook

这意味着:

- Claude CLI 自己承认这些能力存在
- 只是协议没有给出像 Codex `thread/compact/start`、`review/start` 那样的结构化 RPC

建议:

- 不要再把 `/compact`、`/review` 写成“后端绝对做不到”
- 先做 live probe，验证 stream-json session 下把 `/compact`、`/review` 当成用户输入发送时，CLI 是否会稳定进入内建流程
- 如果验证通过，再做一层 Claude command adapter:
  - Feidex `/compact` -> Claude CLI `/compact`
  - Feidex `/review` -> Claude CLI `/review`

这里要接受一个现实:

- Claude 这类能力更像“CLI slash command 适配”
- 不会天然得到 Codex 那种结构化 review item 生命周期

### 7. `/skills` 对 Claude 不是零基础

协议层至少暴露了这些 capability:

- init 里有 `skills`
- init 里有 `agents`
- init 里有 `plugins`
- 可用工具列表里还能看到 `Skill`

所以:

- Claude 当前并非“完全没有技能生态”
- 当前只是 Feidex 的 `/skills` UI 完全绑在 Codex `skills/list`

建议:

- v1 先做 Claude-native 的只读技能卡，展示 `skills` / `agents` / `plugins`
- v2 再把 `$skill-name ...` 的校验从 Codex `skills/list` 改成 Claude session init 公告的 `skills`
- v3 如果后续确认 Skill tool 的发现/调用约定，再补选择交互

### 8. `/history` 更像应用持久化问题，不是纯协议问题

上游 SDK 已经给了这些方向:

- `getTurnHistory()`
- session recording / `loadRecording(...)`
- 消息自动流式记录到磁盘
- hook 输入里还会带 `transcript_path`

而 Feidex 本地 `internal/claudecli` 目前没有把这些能力完整搬进来。

所以:

- Claude 没有 Codex 那种 `thread/read` RPC
- 但并不等于 Feidex 不能做 `/history`

建议:

- 最小实现: 在 Feidex state 里持久化每个 Claude turn 的输入、final text、duration、usage
- 完整实现: 补 session recording / transcript 索引，再做 richer 的 `/history detail`

## Feidex 本地 wrapper 现在漏接了什么

对照 `claude-cli-protocol` 和当前 `internal/claudecli`，最大的差距不是 Claude CLI 没能力，而是本地 wrapper 还没把不少字段和控制面接出来。

### 已经接上的

- `--resume`
- `get_context_usage`
- permission callback
- `AskUserQuestion`
- `ExitPlanMode`

### 还没接上的重要信息

| 能力 | 协议 / 上游 SDK 情况 | Feidex 当前情况 |
| --- | --- | --- |
| `slash_commands` | init message 提供 | `internal/claudecli.SessionInfo` 没有保留 |
| `agents` / `skills` / `plugins` | init message 提供 | 本地 wrapper 没有保留 |
| `modelUsage.contextWindow` | `ResultMessage` 提供 | 已接入，用于 Claude final footer 的同步 context 计算 |
| `set_model` | 上游 Go SDK 已实现 | `internal/claudecli` 没有实现 |
| `set_permission_mode` | 上游 Go SDK 已实现 | `internal/claudecli` 没有实现 |
| `fork_session` | 协议配置存在 | `internal/claudecli` 没有实现 |
| sandbox settings | 协议配置存在 | `internal/claudecli` 没有实现 |
| recording / loadRecording | 上游 SDK 已实现 | Feidex 本地 wrapper 未提供 |
| turn history API | 上游 SDK 已实现 | Feidex 本地 wrapper 未提供 |

这张表对应的优先级其实很清楚:

- 第一优先级: 接出 `slash_commands`、`agents`、`skills`、`plugins`、`modelUsage`
- 第二优先级: 补 `set_permission_mode` 和 `set_model`
- 第三优先级: 再做 `fork_session`、recording、history/catalog

## 实现建议

### P0: 先修 UX 误导

- 给 `/help` 加 backend-aware 过滤，至少要把 Claude 不支持的命令标成“Codex only”。
- 给菜单加 capability filter 或 disabled 标记，不要让 Claude 用户反复点进死胡同。
- 在 Claude 上直接去掉或禁用这两个误导入口:
  - `menu.review`
  - `menu.group.model`
- `menu.tools` 至少应对 `review`、`compact`、`history`、`skills` 做禁用或注记。
- 同时把 Claude init 返回的 `slash_commands`、`skills`、`agents`、`plugins` 接进 `SessionInfo`，让 `/help` 和状态面板优先用真实 capability 驱动。

这是成本最低、收益最高的一层。

### P1: 做一个统一能力表，不要分散在多个 if / switch

- 建议引入 backend capability registry，至少覆盖:
  - slash command 可用性
  - menu action 可用性
  - help 是否展示
  - 展示文案里的 backend 注记
- 这样 `/help`、菜单、报错信息、文档都能从同一份事实来源生成。
- 现在同一个事实散落在 `claudeUnsupportedCommand()`、`commandThread()`、菜单 action handler 里，后续很容易再次漂移。

### P2: 先把“可显示但表述错误”的能力补齐

- `status` 应改成 backend-aware:
  - Claude 下不要继续强调 `全局模型`、`全局推理强度`、`thread service tier` 这些 Codex 口径字段。
  - Claude 下应优先展示协议真实返回的 `model`、`permissionMode`、`tools`、`slash_commands`、`plugins`。
- `menu.thread` 的 Claude 卡可以保留，但建议明确区分“Claude session”与“Codex thread”。
- `ResultMessage.modelUsage` 已解析，Claude final footer 直接用 `contextWindow` + `ResultMessage.usage` 同步计算 context usage，不再异步请求 `get_context_usage`。

### P3: Claude skills 可以做，但要换实现路径

- 如果产品希望 Claude 也能选 skill，不应继续复用 Codex 的 `skills/list`。
- 更合理的做法是抽一个 backend-neutral skill catalog 接口:
  - Codex adapter 继续走 `skills/list`
  - Claude adapter 先走 session init 公告的 `skills` / `agents` / `plugins`
- 一旦 skill catalog 变成 backend-neutral，下面这些都能自然复用:
  - `/skills`
  - 菜单里的 `技能列表`
  - `$skill-name ...`
  - pending skill 选择

这里的关键点是: Claude prompt 注入层已经在，缺的是“发现与选择”。

### P4: 把“协议已经给原语”的命令优先补齐

建议优先级:

1. `/thread policy`
2. `/model`
3. `/thread resume` + `/threads`
4. `/skills`
5. `/compact` / `/review`
6. `/thread fork`
7. `/history`

原因:

- 1 和 2 最接近现成控制面
- 3 缺的是 catalog，不缺 resume 原语
- 4 已有 init capability 可读
- 5 CLI 已广告 slash commands，但要先 live probe
- 6 有协议配置线索，但还缺验证
- 7 主要是应用持久化工程量

### P5: 其余能力需要单独设计，而不是简单解封

| 能力 | 建议 |
| --- | --- |
| `/history` | 不要等 Claude 提供 `thread/read`；直接做 Feidex 自己的 turn/session 持久化。 |
| `/compact` | 先 live probe Claude slash command `/compact`；如果 stream-json 下稳定，再做命令适配器。 |
| `/review` | 先 live probe Claude slash command `/review` / `security-review`；做 Claude-only 适配，不要伪装成 Codex `review/start`。 |
| `/model` | 直接基于 `--model` / `set_model` 做 Claude 专属配置，不要继续写 `config.Codex`。 |
| `/fast` | 目前只有 read-only `service_tier` 痕迹，没有稳定可写控制面；建议继续隐藏。 |
| `/thread list/resume` | 依赖 Feidex 自己维护 session catalog，不依赖 CLI RPC。 |
| `/thread fork` | 先验证 `fork_session` 的真实行为，再决定是否产品化。 |
| `/thread sandbox` | 需要重做 Claude-specific sandbox UI 和映射，不能复用 Codex sandbox 语义。 |
| `/thread policy` | 直接接 `set_permission_mode`，这是最容易先补齐的一项。 |

## 当前最准确的产品表述

如果只用一句话概括当前状态，最准确的说法应是:

> Claude backend 当前对用户暴露出来的能力仍然偏少，但从 Claude CLI protocol 看，并不是很多功能“后端完全没有”，而是 Feidex 还没把 session resume、permission mode、model control、skills/agents/plugins capability、slash command 适配、history 持久化这些能力接出来。真正应该继续隐藏的，主要是缺少稳定控制面的 `fast` 和尚未定义好语义映射的 sandbox / fork。
