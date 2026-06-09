# 配置参考

Feidex 使用 TOML 配置文件，默认读取当前目录的 `config.toml`。完整样例见 [config.example.toml](../config.example.toml)。

## 最小配置示例

```toml
data_dir = ".feidex-data"

[log]
level = "info"

[feishu]
app_id = "cli_xxx"
app_secret = "sec_xxx"
allow_from = []
debug_allow_from = []
group_at_only = true
respond_to_at_everyone = false
card_enabled = true
reply_in_thread = true
quiet = "progress"

[codex]
command = "codex"
transport = "stdio"
# app_server_dir = "/srv/shared-codex-dir"
experimental_api = true
service_name = "feidex"

# 可选：Claude Code 后端
# [claude]
# command = "claude"
# model = "sonnet"
# permission_mode = "default"

[daemon]
service_name = "feidex"

# 可选：多前端配置（替换单一 [feishu]）
# [[frontend]]
# id = "codex-main"
# backend = "codex"
# app_id = "cli_xxx"
# app_secret = "sec_xxx"
#
# [[frontend]]
# id = "claude-main"
# backend = "claude"
# app_id = "cli_yyy"
# app_secret = "sec_yyy"

[[workspace]]
id = "default"
name = "Default"
cwd = "."
approval_policy = "on-request"
sandbox_mode = "workspace-write"
# claude_permission_mode = "default"
```

## `data_dir`

本地状态目录，默认 `.feidex-data`。

Feidex 会把这些状态写进去：

- sessions
- submissions
- pending requests
- message links

## `[feishu]`

- `app_id` / `app_secret`
  - 飞书应用凭据
- `allow_from`
  - 允许的用户列表；空表示不限制
- `debug_allow_from`
  - 允许使用 debug 功能的用户列表；控制 `/debug` 和 `/debug logs`，空表示默认不允许任何用户使用
- `group_at_only`
  - 群聊是否仅在 `@bot` 时响应
- `respond_to_at_everyone`
  - 是否响应 `@所有人`
- `card_enabled`
  - 是否启用卡片输出
- `reply_in_thread`
  - 群聊是否在线程内回复
- `quiet`
  - Quiet 模式默认值，默认 `progress`

## `[codex]`

- `command`
  - 默认是 `codex`
- `transport`
  - 只支持 `stdio`
- `experimental_api`
  - 当前建议保持 `true`
- `service_name`
  - 提交到 Codex 的 service name
- `app_server_dir`
  - 启动 `codex app-server` 的工作目录
- `model`
  - Codex 全局模型
- `reasoning_effort`
  - Codex 全局推理强度
- `plan_model`
  - Plan 模式专用模型；留空时跟随 default mode 的模型解析链路
- `plan_reasoning_effort`
  - Plan 模式专用推理强度；留空时跟随 app-server 的 plan preset，若 preset 未提供则不额外发送

## `[claude]`

Claude Code 后端配置：

- `command`
  - Claude CLI 路径，默认 `claude`
- `model`
  - 模型名称，默认 `sonnet`（可选 `opus`、`haiku`）
- `effort`
  - reasoning effort：`low`、`medium`、`high`、`xhigh`、`max`（留空 = 自动）
- `permission_mode`
  - 权限模式：`default`、`acceptEdits`、`plan`、`bypassPermissions`
- `dangerously_skip_permissions`
  - 启用 `bypassPermissions` 前必须设为 `true`
- `disable_plugins`
  - 禁用 Claude Code 插件
- `system_prompt`
  - 自定义 system prompt
- `permission_prompt_tool_stdio`
  - 是否在 tool stdio 时提示（默认 true）

## `[[frontend]]`

多前端配置（可选，替换单一 `[feishu]` 配置）。每个 frontend 可运行独立的飞书 bot：

- `id`
  - 前端标识符
- `backend`
  - 后端选择：`"codex"` 或 `"claude"`
  - 留空则在启动时弹出交互式选择卡片
- `app_id` / `app_secret`
  - 该前端的飞书应用凭据
- `allow_from` / `debug_allow_from` / `group_at_only` 等
  - 与 `[feishu]` 下的同名字段含义相同
- `feidex feishu setup/new/bind --frontend-id <id> [--backend ...]`
  - 可直接创建或更新这些 `[[frontend]]` 条目，无需手动编辑 TOML

## `[daemon]`

- `service_name`
  - Linux daemon 的 systemd user service 名称，默认 `feidex`
  - 同机多实例时应保证每套配置使用不同的 `service_name` 和 `data_dir`

## `[[workspace]]`

支持多个 workspace，每个 workspace 可以有自己独立的：

- `cwd`
- `approval_policy`
- `sandbox_mode`
- `claude_permission_mode`（Claude 权限模式，留空跟随全局默认）

## 飞书接入命令

Feidex 提供了三种飞书配置方式：

```bash
feidex feishu setup [--config config.toml] [--frontend-id id] [--backend codex|claude]
feidex feishu new   [--config config.toml] [--frontend-id id] [--backend codex|claude]
feidex feishu bind  --app app_id:app_secret [--config config.toml] [--frontend-id id]
```

说明：

- `feishu setup`
  - 自动模式
  - 如果你提供了现有 app 凭据，就走绑定
  - 否则走新建应用的二维码授权流程
- `feishu new`
  - 强制新建
- `feishu bind`
  - 绑定已有飞书应用
- `--frontend-id`
  - 直接创建或更新指定 `[[frontend]]`
  - 如果当前配置还在用顶层 `[feishu]`，第一次增加命名 frontend 时会自动迁移成 `[[frontend]]`
- `--backend`
  - 给新建或更新的 frontend 预填 backend（`codex` 或 `claude`）

多 frontend 示例：

```bash
feidex feishu setup --config config.toml --frontend-id codex-main --backend codex
feidex feishu setup --config config.toml --frontend-id claude-main --backend claude
```

对应实现见：

- [feishu.go](../cmd/feidex/feishu.go)
- [feishu_setup.go](../internal/config/feishu_setup.go)
