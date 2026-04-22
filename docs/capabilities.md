# Feidex Backend Capability Matrix

> 更新时间: 2026-04-22
>
> 口径:
> - 只描述“当前 frontend 已选定 backend，且不在维护模式”时的正常行为。
> - 以当前 `internal/app` 实现和测试为准，不延续旧版“Claude 大量缺失本地能力”的结论。
> - 这里讨论的是 Feidex 的本地产品能力，不等同于底层 backend 理论上能否支持某项协议。

主要代码 / 测试来源:

- `internal/app/commands.go`
- `internal/app/command_registry.go`
- `internal/app/menu_specs.go`
- `internal/app/menu_actions.go`
- `internal/app/backend_capability_facade.go`
- `internal/app/conversation_backend_facade.go`
- `internal/app/claude_runtime.go`
- `internal/app/claude_session_catalog.go`
- `internal/app/claude_model_config.go`
- `internal/app/claude_permission_config.go`
- `internal/app/workspace_menu.go`
- `internal/app/commands_test.go`
- `internal/app/menu_command_direct_access_test.go`
- `internal/app/claude_history_test.go`
- `internal/app/claude_session_catalog_test.go`
- `internal/app/model_config_test.go`
- `internal/app/app_more_test.go`

## 总规则

- `/help` 和菜单现在都按 backend 过滤，不再展示当前 backend 不拥有的本地能力。
- `Codex` 路径使用 `thread` 术语；`Claude` 路径使用 `session` 术语。帮助文案、菜单标题、摘要文案都会跟着 backend 改写。
- `/new` 和 `/fork` 仍然保留为共享别名，但落到不同 backend 时，会分别映射到 `thread` 或 `session` 语义。
- “当前 backend 不本地处理”的 slash 命令，并不一定报错。`handleCommand()` 会把原始文本当普通输入提交给当前 backend。
- Claude 的 model / effort 切换、backend 切换，都是 frontend 级的空闲态操作；不允许在 active work、排队输入或 pending form 存在时切换。

## 能力状态定义

- `本地处理`: Feidex 拦截 slash/menu，并由本地逻辑执行。
- `本地入口，backend 特化`: 命令入口仍由 Feidex 拦截，但具体实现按 backend 分流。
- `非本地`: 当前 backend 不把这条 slash 当作 Feidex 本地命令；如果用户手动输入，会被当作普通消息转发给 backend。

## 核心能力矩阵

| 能力 | Codex backend | Claude backend | 说明 |
| --- | --- | --- | --- |
| `/menu`、`/help` | 本地处理 | 本地处理 | 两端都走同一套注册表，但会按 backend 过滤显示项。 |
| 根菜单分组过滤 | 支持 | 支持 | `menu_specs.go` 基于当前 backend 过滤按钮；不再是“全量静态暴露”。 |
| `/backend` | 本地处理 | 本地处理 | frontend 级 backend 选择与切换。 |
| `/quiet`、`/debug`、`/download`、`/upgrade`、`/codex`、`/claude`、`/status` | 本地处理 | 本地处理 | 这些都是 Feidex 本地产品能力。 |
| `/interrupt`、`/stop` | 本地入口，backend 特化 | 本地入口，backend 特化 | Codex 走 `turn/interrupt`；Claude 走 Claude runtime interrupt。 |
| `/history` | 本地入口，backend 特化 | 本地入口，backend 特化 | Codex 读 `thread/read`；Claude 读本地 session transcript。 |
| `/usage` | 本地入口，backend 特化 | 本地入口，backend 特化 | 两端都可用，但来源不同。 |
| `/model`、`/effort` | 本地入口，backend 特化 | 本地入口，backend 特化 | Codex 配 `config.Codex`；Claude 配 `config.Claude` 并立即重置当前 frontend 的 Claude 会话。 |
| `/compact` | 本地处理 | 本地入口，Claude passthrough | Codex 走 `thread/compact/start`；Claude 会把原始 `/compact` 文本提交给 Claude backend。 |
| `/review` | 本地处理 | 非本地 | Claude 不再把它当本地命令；手动输入时会原样提交给 Claude。 |
| `/skills` | 本地处理 | 非本地 | Claude 侧没有本地 skills 发现/选择 UI。 |
| `/fast` | 本地处理 | 非本地 | `fast` 当前仍是 Codex thread service tier 语义。 |
| `/thread ...` | 本地处理 | 非本地 | Claude 已用 `/session ...` 替代，不再本地接管 `/thread`。 |
| `/threads` | 本地处理 | 非本地 | Claude 帮助和菜单不再展示 `/threads`。 |
| `/session ...` | 非本地 | 本地处理 | Claude 本地会话管理入口。 |
| `/workspace sandbox`、`/workspace policy` | 本地处理 | 非本地 | Claude 工作区默认配置改为 `permissions`。 |
| `/workspace permissions` | 非本地 | 本地处理 | Claude 工作区默认权限模式入口。 |

## 会话 / 线程能力

### Codex

- 主术语是 `thread`。
- 本地命令入口:
  - `/thread`
  - `/thread list [all]`
  - `/threads`
  - `/thread new`
  - `/thread fork`
  - `/thread resume THREAD_ID`
  - `/thread sandbox [MODE]`
  - `/thread policy [POLICY]`
  - `/new`
  - `/fork`
- `fork` 走 `thread/fork`，并保留当前 thread 的 sandbox、approval policy、service tier。

### Claude

- 主术语是 `session`。
- 本地命令入口:
  - `/session`
  - `/session list [all]`
  - `/session new`
  - `/session fork`
  - `/session resume SESSION_ID`
  - `/session permissions [MODE|inherit]`
  - `/new`
  - `/fork`
- 本地会话列表和恢复能力已经接入，不再是“理论可做、产品没接”。
- 会话目录来源于 Claude 本地 transcript 目录扫描，当前实现读取 `CLAUDE_CONFIG_DIR/projects` 或默认 `~/.claude/projects`。
- `fork` 已接入 Claude runtime 的 fork session 语义；如果新的 `session_id` 还没 materialize，会先进入“待下一条消息创建”的过渡态。

## 历史记录

- `/history` 在两端都是本地能力，但数据源不同。
- Codex:
  - 通过 `thread/read` 读取 turn 历史。
  - `history detail TURN_NUMBER` 解析的是 thread turn 序号。
- Claude:
  - 直接读取本地 session transcript JSONL。
  - `history detail TURN_NUMBER` 同样可用，且按 Claude turn ordinal 解析。
- 因此旧结论“Claude 没有 `/history`”已经失效。

## 模型与推理强度

### Codex

- `/model`
- `/model set <model-id|default>`
- `/model effort <effort|default>`
- `/effort`

Codex 路径仍基于 `model/list` 与 `config.Codex`。

### Claude

- `/model`
- `/model set <model-id|default>`
- `/model effort <effort|default>`
- `/effort`

Claude 路径已经是本地能力，不再需要降级成“不可用”结论。

- UI picker 提供常用别名和当前自定义 model。
- 任意 raw model 仍可直接用 `/model set <model-id>`。
- 切换 model / effort 必须在当前 frontend 空闲时进行。
- 切换成功后会立即更新 runtime 配置，并重置当前 frontend 的 Claude 会话。

## 权限 / sandbox / policy

### Codex

- conversation 级:
  - `/thread sandbox [MODE]`
  - `/thread policy [POLICY]`
- workspace 级:
  - `/workspace sandbox [MODE]`
  - `/workspace policy [POLICY]`

这些都是 Codex thread 语义下的本地能力。

### Claude

- conversation 级:
  - `/session permissions [MODE|inherit]`
- workspace 级:
  - `/workspace permissions [MODE|inherit]`

Claude 当前不暴露 Codex 风格的 sandbox / approval policy 菜单与命令，而是收敛到 permission mode。

当前权限模式入口支持的语义包括:

- `default`
- `acceptEdits`
- `auto`
  - 仅当本机 Claude CLI 支持时可见；不支持时会回退。
- `bypassPermissions`
- `inherit`
  - 仅用于清除 session / workspace 覆盖。

## 菜单与帮助现在的真实状态

- Claude 的 `/help` 已经不会再展示这些本地能力:
  - `/review`
  - `/skills`
  - `/fast`
  - `/thread`
  - `/threads`
  - `/workspace sandbox`
  - `/workspace policy`
- Claude 的 `/help` 现在会展示这些已经落地的本地能力:
  - `/history`
  - `/compact`
  - `/session fork`
  - `/session resume SESSION_ID`
  - `/session permissions`
  - `/workspace permissions`
- Claude 的菜单卡也已经按 backend 过滤:
  - 常用工具卡不再显示 `review`、`skills`
  - 模型卡保留 `/model`，移除 `/fast`
  - 会话卡显示 `/session ...`、`/session permissions`
  - 工作区卡显示 `/workspace permissions`，不显示 `sandbox/policy`

因此旧结论“菜单和 `/help` 没有 backend filter”已经失效。

## 当前仍然是 backend 专属的本地能力

### 仅 Codex 本地拥有

- `/review ...`
- `/skills`
- `/skills reload`
- `$skill-name <内容>` 的发现 / 选择链路
- `/fast ...`
- `/thread sandbox ...`
- `/thread policy ...`
- `/workspace sandbox ...`
- `/workspace policy ...`

### 仅 Claude 本地拥有

- `/session ...`
- `/session permissions ...`
- `/workspace permissions ...`
- 基于本地 transcript 的 session list / history

## 关于 Claude 的两个重要边界

### 1. “不是本地能力”不等于“不能输入”

在 Claude backend 下，`/review`、`/skills`、`/fast config`、`/thread`、`/workspace sandbox`、`/workspace policy` 这类命令不会作为 Feidex 本地命令处理，但如果用户手动输入，Feidex 会把原始文本作为普通 prompt 提交给 Claude。

这意味着:

- 菜单 / `/help` 会隐藏它们。
- 旧卡片里的陈旧按钮如果还指向这些命令，也会退化成 passthrough，而不是本地 hard block。

### 2. skills 对 Claude 不是“完全不可能”

- `Submission.Skills` 注入 Claude prompt 的基础 plumbing 已经存在。
- 但当前用户侧的 skills 发现 / 选择仍依赖 Codex 的 `skills/list`。

所以当前更准确的表述是:

- Claude 没有本地 skills 发现 / 选择 UI。
- 不是 Claude prompt 注入层完全不支持 skills。
