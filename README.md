# Feidex

Feidex 是一个把 Codex App Server / Claude Code 接到飞书消息流上的中间层服务。

它的目标不是做一个通用聊天机器人，而是把「在本机/服务器上运行的 Codex/Claude 能力」以飞书对话的形式暴露出来，让你可以在飞书里：

- 发起 Codex 或 Claude 任务
- 在同一个 thread/session 里继续多轮交互
- 查看线程/会话历史、状态、工作区、模型配置
- 审批命令/文件/权限请求
- 在 Codex 和 Claude 后端之间动态切换
- 通过 daemon 模式做后台运行与自升级

项目面向中文使用场景，README 也以中文为主。

开发约束、模块边界、构建产物放置规则见 [DEVELOPER.md](DEVELOPER.md)。
Codex App Server 协议状态机约束见 [docs/codex-app-server-state-machine-audit.md](docs/codex-app-server-state-machine-audit.md)。

## 文档口径

- README 只描述“当前 release 的长期能力、当前行为和使用方式”，不承担按版本罗列增量的职责。
- 当前 README 以 `v0.213.0` 的产品状态为准。
- 版本级新增能力、行为调整和修复记录见 GitHub Releases。

## 主要能力

- 飞书消息接入
  - 支持单聊与群聊
  - 群聊可配置为仅在 `@bot` 时响应
  - 支持回复树内的会话连续性
  - 多前端（`[[frontend]]`）支持单进程运行多个飞书 bot
- 双后端支持：Codex + Claude
  - Codex：thread / turn 模型，`/thread` 命令族，审批流程
  - Codex：`/plan` collaboration mode 与计划完成后的实现确认流程
  - Claude：session / conversation 模型，`/session` 命令族，权限模式
  - 在线后端切换：`/backend` 命令，无需重启
  - 后端特定的 CLI 升级：`/codex`、`/claude` 命令
- 会话与队列管理
  - 新消息默认 queue / 新 turn（Codex）或新 message（Claude）
  - 回复消息支持 steer 到当前 root 绑定的 turn
  - steer 失败时自动回退到 queue
  - 支持暂存附件（不仅图片）；下一条文字会把暂存附件一起带入输入
  - 支持自动重试（auto-retry）失败线程
- 菜单卡片
  - 单卡片内导航
  - 面包屑路径
  - 子菜单入口带展开标识
  - 真实命令项会显示对应 slash 命令
- 工作区管理
  - 多 workspace
  - workspace 级别 sandbox / approval policy / Claude permission mode
  - thread/session 级别 sandbox / policy / service tier / permission mode
  - 支持 workspace 克隆（`/workspace clone`）、删除、选择
  - 支持 workspace 范围内的路径选择与文件下载分享
- 审批与补充输入
  - 命令审批
  - 文件变更审批
  - 权限审批
  - request_user_input / elicitation 表单
  - 单题少选项时自动走 quick-card 按钮，其余场景走表单
  - quick-card 正文会重复完整选项说明，避免手机端按钮文案被截断后看不清
- Code Review（Codex only）
  - `/review` 未提交变更
  - `/review base` / `/review commit` 指定范围
  - `/review custom` 自定义审查指令
- 技能支持（Codex only）
  - `/skills` 查看可用技能
  - `$skill-name <content>` 前缀语法指定技能
- 诊断与可观测性
  - 运行时日志级别切换与最近日志查看
  - thread/session token usage / context left 展示
  - 飞书权限问题卡片化提示
- 运行与升级
  - Linux 用户态 systemd daemon
  - `feidex daemon logs` 直接查看 daemon 日志
  - GitHub Release 自升级
  - 自动按本机架构选择 `amd64` 或 `aarch64` 资产
  - 支持 `/upgrade [VERSION]` 直接指定目标版本
  - 支持 `/upgrade dev` 升级到 `dev-latest`
  - 支持 `/upgrade local`/`/upgrade path` 本地二进制升级
- 发布
  - 自带打 tag 脚本
  - 可自动从 GitHub 远端 tag 推导下一个 minor 版本
  - 同时生成 Linux (amd64/aarch64) 和 macOS (amd64/arm64) 二进制

## 目录结构

```text
cmd/feidex/                 主程序入口
cmd/feishu_card_demo/       飞书卡片 demo
internal/app/               应用协调层（frontend、session、菜单、审批、turn lifecycle）
internal/app/appcore/       核心组合（client 接口、session key、workspace 选择）
internal/app/apphistory/    进程历史
internal/app/appstate/      应用状态 store
internal/app/approval/      审批卡片文案、按钮、文件摘要
internal/app/approvalview/  审批视图渲染
internal/app/autoretry/     自动重试状态
internal/app/backend/       后端驱动抽象（选择、action、failure、transition）
internal/app/cards/         飞书卡片构造 helper
internal/app/clauderuntime/ Claude 运行时集成
internal/app/claudesession/ Claude session 生命周期
internal/app/claudesupport/ Claude 诊断/历史 helper
internal/app/codexruntime/  Codex 运行时集成
internal/app/compact/       上下文压缩
internal/app/convbackend/   会话后端 facade
internal/app/delivery/      回复卡片分片、markdown 拆分
internal/app/features/      统一命令/菜单/action 注册
internal/app/feishuwrap/    飞书适配包装器
internal/app/finalcardpatch/最终卡片 patch 逻辑
internal/app/historycmd/    历史命令处理
internal/app/lifecycle/     pending request / lifecycle 共享谓词
internal/app/maintenance/   backend-agnostic 升级/维护 workflow
internal/app/modelconfig/   模型配置流
internal/app/pathpick/      路径选择器
internal/app/pendingforms/  待处理表单
internal/app/replycontinuation/ 回复接续处理
internal/app/review/        review target 数据结构
internal/app/reviewcmd/     review 命令处理
internal/app/serverrequest/ 服务端请求处理
internal/app/sessionctx/    session 上下文类型
internal/app/skills/        技能管理
internal/app/skillscmd/     技能命令处理
internal/app/submission/    submission 队列与生命周期
internal/app/threadmenu/    thread 菜单渲染
internal/app/threadview/    thread 视图渲染
internal/app/turn/          turn 管理
internal/app/turnbinding/   turn 绑定逻辑
internal/app/turnitem/      turn item 类型
internal/app/turnlifecycle/ turn 生命周期协调
internal/app/turnstream/    turn 流处理
internal/app/upgradecmd/    升级命令处理
internal/app/upgraderender/ 升级卡片渲染
internal/app/usageview/     usage 视图渲染
internal/app/workspace/     workspace payload 与值对象
internal/app/workspacecmd/  workspace 命令处理
internal/feishu/            飞书适配层
internal/codexrpc/          Codex App Server RPC 客户端与类型
internal/claudecli/         Claude CLI stream-json 适配层
internal/config/            配置与飞书绑定流程
internal/state/             本地状态存储（session/submission/message links）
internal/daemon/            daemon 安装、运行与升级
internal/release/           GitHub Release 查询与版本比较
internal/codexinstall/      Codex CLI 安装与探测
internal/claudeinstall/     Claude CLI 安装与探测
scripts/create_github_release.sh  发布 tag 脚本
config.example.toml         配置样例
```

## 架构视图

当前主路径是：飞书事件进入 `internal/feishu`，由 `internal/app` 做 frontend/session/thread 归属、命令分发、审批与卡片渲染，再通过 backend runtime facade 调用 `internal/codexrpc` 或 `internal/claudecli`，运行时和可恢复状态写入 `internal/state`。

关键边界：

- `frontend` 是运行时隔离边界；backend 选择、session lineage、pending request、message link 和运行时缓存都必须按 frontend 隔离。
- `internal/app` 拥有产品语义；Codex/Claude 协议细节应收敛在 backend adapter/facade（`internal/app/backend/`），避免散落到消息、菜单和审批编排里。已物理拆出的 50+ 子包只承载无 `*App` 依赖的纯逻辑、值对象或窄职责 helper。
- `internal/codexrpc` 只负责 Codex App Server 传输和协议类型；`internal/claudecli` 只负责 Claude CLI stream-json 协议。不要让它们理解飞书、session 或卡片。
- 命令与菜单通过 `internal/app/features/` 统一注册，按 backend 自动过滤可用命令。
- app 物理子包的职责边界见 [docs/app-package-boundaries.md](docs/app-package-boundaries.md)；新增子包不得反向 import `internal/app`。
- `internal/feishu` 只负责飞书 SDK、消息/卡片发送、文件分享、链接改写和权限问题转换；不要把业务策略放进适配层。
- 慢操作必须走"快速 callback ack → 异步执行 → patch card / follow-up"，尤其是 clone、review、upgrade、download 和外部网络请求。
- 异步操作使用 `RunAsync` + `sync.WaitGroup` 追踪，测试通过 `a.waitAsync()` 同步而非 `time.Sleep`。
- 触碰 `internal/app`、`internal/codexrpc`、`internal/claudecli`、审批、turn/thread lifecycle、review、compaction、tool input 或 server request 时，要同步检查状态机审计文档。

### 目前的架构问题

- God package 拆分基本完成：`internal/app` 已从单一巨型 package 收敛为 50+ 子包，卡片渲染、升级流程、审批、命令行、review、compaction、turn 生命周期等各有关键包。剩余的高耦合区域主要在 lifecycle coordinator 和多后端共享状态。
- backend 抽象通过 `backend/driver.go` + `backendRuntimeFacade` 统一，但部分 Codex/Claude 专用状态仍在 `internal/app`。新增 backend 时应先补 facade，不要在命令和卡片路径继续增加 `if backend == ...`。
- 状态同时存在内存 map 与 `internal/state` 持久化快照。新增 pending/form/message-link/session 数据时，必须明确 frontend scope。
- README、`DEVELOPER.md` 和状态机审计共同构成开发契约。协议行为变化不能只改代码。
- 升级链路是救援路径，相关改动要保持 daemon/release/pending store 最小依赖。

## 运行前提

### 必需

- Go 1.23+（按 `go.mod` 为准）
- 已安装 `codex` CLI，并且 `codex app-server` 可启动
- 一个可用的飞书应用 `app_id / app_secret`

### 可选

- Linux + systemd 用户服务
  - 如果你希望 Feidex 长驻后台并支持自升级，推荐使用 daemon 模式

## 快速开始

### 1. 构建

```bash
mkdir -p bin
go build -o bin/feidex ./cmd/feidex
```

### 2. 准备配置

复制样例：

```bash
cp config.example.toml config.toml
```

最小配置示例：

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

### 3. 启动

```bash
./bin/feidex serve --config config.toml
```

或者直接：

```bash
./bin/feidex
```

## 飞书接入

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

- [feishu.go](cmd/feidex/feishu.go)
- [feishu_setup.go](internal/config/feishu_setup.go)

## 配置说明

### `data_dir`

本地状态目录，默认 `.feidex-data`。

Feidex 会把这些状态写进去：

- sessions
- submissions
- pending requests
- message links

### `[feishu]`

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

### `[codex]`

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

### `[claude]`

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

### `[[frontend]]`

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

### `[daemon]`

- `service_name`
  - Linux daemon 的 systemd user service 名称，默认 `feidex`
  - 同机多实例时应保证每套配置使用不同的 `service_name` 和 `data_dir`

### `[[workspace]]`

支持多个 workspace，每个 workspace 可以有自己独立的：

- `cwd`
- `approval_policy`
- `sandbox_mode`
- `claude_permission_mode`（Claude 权限模式，留空跟随全局默认）

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
  - 查询 npm 最新 Codex CLI 版本
- `/codex upgrade`
  - 升级 Codex CLI（失败自动回退）
- `/codex restart`
  - 重启 Codex 运行时（空闲时）
- `/claude`
  - 检查 Claude CLI 安装/升级状态
- `/claude check`
  - 查询 npm 最新 Claude CLI 版本
- `/claude upgrade`
  - 升级 Claude CLI（含 smoke test，失败自动回退）
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

## Daemon 模式

Feidex 支持 Linux 用户态 systemd daemon。

命令：

```bash
feidex daemon install
feidex daemon enable-linger
feidex daemon start
feidex daemon stop
feidex daemon restart
feidex daemon status
feidex daemon logs [-n 50] [-f]
feidex daemon uninstall
```

说明：

- `install`
  - 安装并启动用户服务；默认读取当前目录的 `config.toml`
  - 安装前会默认尝试为当前用户打开 linger；如果你不希望修改这个用户级 systemd 设置，可改用 `feidex daemon install --disable-linger`
- `enable-linger`
  - 手动为当前用户打开 linger；通常不需要单独执行，`install` 默认就会尝试开启
- `start` / `stop` / `restart` / `status` / `uninstall`
  - 也默认读取当前目录的 `config.toml`，按其中的 `[daemon].service_name` 选择实例
- `logs`
  - 通过 `journalctl --user -u <service>.service` 查看当前 daemon 日志
  - `-n` 控制输出行数，`-f` 持续跟随
  - 依赖 Linux/systemd journald
- `upgrade-runner`
  - 内部升级 runner，不需要手动调用

对应实现见：

- [daemon.go](cmd/feidex/daemon.go)
- [manager.go](internal/daemon/manager.go)

## 自动升级

自动升级要求：

- 当前运行在 Linux daemon 模式
- 服务进程正在运行
- 如果走 GitHub release 流程，需要可访问 GitHub Release
- release 也会发布 macOS 产物，但自动升级仍只支持 Linux daemon

升级逻辑会：

- `/upgrade`
  - 查询最新 release
- `/upgrade dev`
  - 查询 `dev-latest` prerelease，获取当前 main 分支最近一次 push 产物
- `/upgrade vX.Y.Z`
  - 直接查询指定 tag，跳过最新版本探测
- `/upgrade local`
  - 打开当前 workspace 的文件选择器，选择本地 Binary
- `/upgrade path ./dist/feidex-linux-amd64`
  - 直接使用当前 workspace 下的本地 Binary，跳过 GitHub 查询
- 根据本机架构选择正确二进制
  - `amd64 -> feidex-linux-amd64`
  - `arm64 -> feidex-linux-aarch64`
- macOS release 对应为
  - `amd64 -> feidex-darwin-amd64`
  - `arm64 -> feidex-darwin-arm64`
- 对 release 流程校验 `sha256sums.txt`
- 对本地 Binary 流程先把制品复制到 `data_dir/upgrades/<request-id>/` 并计算 `sha256`
- 替换、重启
- 启动失败自动回退

设计约束：

- `/upgrade` 是救援路径；即使线程、工作区、Codex RPC、普通交互流程等其它功能异常，升级链路也必须尽量保持可用
- 升级实现不能依赖其它业务功能的成功执行；允许依赖的前置条件应尽量收敛到 daemon 状态、release 查询、确认卡片和本地 pending store
- 任何改动升级链路的提交，都必须补或更新独立的 upgrade 隔离测试，证明 `/upgrade` 和确认动作在缺失 `codex`/session 运行态时仍能工作

## 发布

### 手动指定版本

```bash
./scripts/create_github_release.sh v0.7.0
```

### 自动推导下一个 minor 版本

```bash
./scripts/create_github_release.sh
```

### 开发版发布

- 每次 `push main`，GitHub Actions 都会刷新 `dev-latest` tag 与同名 prerelease
- 开发版二进制会写入形如 `dev-20260416T233045-<shortsha>` 的版本号，带日期时间，便于 `/upgrade dev` 前后确认具体构建批次

脚本会以 `origin` 的 tag 为准，自动做：

- `v0.4.0 -> v0.5.0`
- `v0.4.3 -> v0.5.0`
- `v0.6.0 -> v0.7.0`

GitHub Actions 会在 tag push 后自动发布 release。

### Release 资产

当前 release workflow 会同时生成：

- `feidex-linux-amd64`
- `feidex-linux-amd64.tar.gz`
- `feidex-linux-aarch64`
- `feidex-linux-aarch64.tar.gz`
- `feidex-darwin-amd64`
- `feidex-darwin-amd64.tar.gz`
- `feidex-darwin-arm64`
- `feidex-darwin-arm64.tar.gz`
- `sha256sums.txt`

## 开发与测试

### Data Race 检测

所有测试必须在 `-race` 下通过：

```bash
go test -race ./...
```

### 异步操作测试

项目使用 `sync.WaitGroup` 追踪所有通过 `RunAsync` 派发的异步 goroutine。测试中调用触发异步操作的函数后，用 `a.waitAsync()` 等待所有 goroutine 完成，然后直接断言——不需要 `time.Sleep` 或 polling 循环。

```go
finishTurn(a, "thread-1", "turn-1", "completed")
a.waitAsync()  // 等待 RunAsync goroutine 完成，状态已落盘
// 直接断言
sess := a.store.GetSession(sessionKey)
if sess.Status != "turn_in_progress" { ... }
```

### 常用测试

```bash
./scripts/with_tmp_go_cache.sh go test -race ./internal/app
./scripts/with_tmp_go_cache.sh go test ./internal/feishu
./scripts/with_tmp_go_cache.sh go test ./internal/state
./scripts/with_tmp_go_cache.sh go test ./internal/release
./scripts/with_tmp_go_cache.sh go test ./internal/daemon
```

Feidex 约定在“默认 Go cache 不可写”或“需要沙箱内临时 cache”时统一使用：

- `GOCACHE=/tmp/feidex-gocache`
- `GOMODCACHE=/tmp/feidex-gomodcache`

优先使用脚本，不要临时发明新的 `/tmp/go-build-*` 或 `/tmp/*-gomodcache` 路径。

等价环境变量写法：

```bash
env GOCACHE=/tmp/feidex-gocache GOMODCACHE=/tmp/feidex-gomodcache go test ./internal/app
```

清理：

```bash
./scripts/clean_tmp_go_cache.sh
```

### 版本信息

```bash
feidex version
```

构建时通过 `-ldflags` 注入：

- `feidex/internal/buildinfo.Version`

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

## 相关文件

- 配置样例：[config.example.toml](config.example.toml)
- 发布脚本：[create_github_release.sh](scripts/create_github_release.sh)
- 主入口：[main.go](cmd/feidex/main.go)
- 飞书绑定：[feishu.go](cmd/feidex/feishu.go)
- daemon 管理：[daemon.go](cmd/feidex/daemon.go)
