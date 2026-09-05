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
card_enabled = true
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
# model_options = ["deepseek-v4-pro"]
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
  - 飞书应用凭据（Lark 国际版同样适用）
- `domain`
  - Open Platform 区域，决定接口域名：`feishu`（默认，`open.feishu.cn`）或 `lark`（国际版，`open.larksuite.com`）
  - 默认即飞书，无需设置；仅 Lark 国际版需要。详见下文 [区域 / Region（飞书 vs Lark）](#区域--region飞书-vs-lark)
- `allow_from`
  - 允许的用户列表；空表示不限制
- `debug_allow_from`
  - 允许使用 debug 功能的用户列表；控制 `/debug` 和 `/debug logs`，空表示默认不允许任何用户使用
- `card_enabled`
  - 是否启用卡片输出
- `quiet`
  - Quiet 模式默认值，默认 `progress`

## 区域 / Region（飞书 vs Lark）

飞书（国内版）与 Lark（国际版）共用同一套 SDK，只是 Open Platform 域名不同。Feidex 用 `[feishu].domain` 切换：

| `domain` | Open Platform 域名 | 适用 |
| --- | --- | --- |
| `feishu`（默认） | `open.feishu.cn` | 飞书（国内版） |
| `lark` | `open.larksuite.com` | Lark（国际版） |

```toml
[feishu]
domain = "lark"
app_id = "cli_xxx"
app_secret = "sec_xxx"
```

多前端时，`domain` 同样可写在每个 `[[frontend]]` 下，互不影响。

绑定 Lark 应用：

```bash
feidex feishu bind --app app_id:app_secret --domain lark
```

注意：

- `feidex feishu new` 的二维码自助注册流程只对 `feishu` 域名可用。Lark 国际版请在 [open.larksuite.com](https://open.larksuite.com) 后台创建应用，再用 `feishu bind --domain lark` 绑定凭据。
- `domain` 只影响 Open Platform 接口域名，不改变任何会话 / 命令 / 卡片行为。

### 创建应用与机器人（官方文档）

| 区域 | 开发者后台 | 创建机器人指南 |
| --- | --- | --- |
| 飞书 | [open.feishu.cn/app](https://open.feishu.cn/app) | [5 分钟开发机器人](https://open.feishu.cn/document/home/develop-a-bot-in-5-minutes/introduction) |
| Lark | [open.larksuite.com/app](https://open.larksuite.com/app) | [Develop a bot in 5 minutes](https://open.larksuite.com/document/home/develop-a-bot-in-5-minutes/introduction) |

接入 Feidex 时，在后台需要：

- 启用 **机器人 / Bot** 能力（卡片菜单/审批需要 **消息卡片**）
- **事件与回调**订阅方式选 **长连接 / persistent connection**（无需公网回调 URL），并订阅 `im.message.receive_v1`、卡片回调 `card.action.trigger`
- **权限 scopes** 至少 `im:message`、`im:message:send_as_bot`；用附件/下载再加 `im:resource`、`drive:drive`
- 配置改动后**创建并发布版本**才生效

> 提示：消息相关 scope 不全时，长连接能建立但**收不到消息事件**（连得上却不回复）。若 bot 不响应，优先核对上面的消息权限并重新发布。

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
  - Codex Bot 默认模型；群聊中可用当前 Bot 在当前群内的模型覆盖
- `reasoning_effort`
  - Codex Bot 默认推理强度；群聊中可用当前 Bot 在当前群内的推理强度覆盖
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
- `model_options`
  - Claude `/model` 下拉框里的额外候选模型名列表
  - 推荐直接在飞书里维护：打开 `/model` 卡片，在“管理候选模型”里添加或移除
  - 也可以用命令：`/model option add <model-id>`、`/model option remove <model-id>`
  - 只影响候选列表，不会切换当前模型；真正切换仍用 `/model set <model-id>` 或卡片下拉选择
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
- `allow_from` / `debug_allow_from` 等
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
