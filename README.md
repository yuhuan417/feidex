# Feidex

> 把你的编码 Agent 装进飞书 —— 在手机上随时发起、续聊和审批 Codex / Claude Code 的任务。

**不用守在终端前，也不用开电脑。** Feidex 把跑在你本机或服务器上的 **Codex App Server / Claude Code** 接到**飞书消息流**上：通勤路上甩一句需求、午饭时审一个改动、睡前看一眼跑完没有 —— 编码 Agent 就在你的聊天框里待命。

它不是又一个通用聊天机器人，而是专注做一件事：**让你用飞书对话来驱动真实的编码 Agent**。

- 💬 **像聊天一样发任务** —— 一条消息就能让 Codex/Claude 开干，回复消息即可多轮续聊
- 🔀 **两个后端，随时切换** —— Codex 与 Claude Code 并存，`/backend` 一键切换，无需重启
- ✅ **重要操作你说了算** —— 命令 / 文件变更 / 权限改动都先弹卡片审批，手机上点一下即可
- 📂 **多工作区、多项目** —— clone 或 Git worktree 即建工作区，同一项目可给不同 bot / 群聊隔离目录
- 🚀 **真正能长跑** —— Linux daemon 后台常驻，`/upgrade` 一键自升级，掉线自动回退

> 面向中文使用场景，文档也以中文为主。

## 特性亮点

- **飞书接入** —— 单聊 / 群聊（可配置仅 `@bot` 响应）、回复树内会话连续性，单进程可跑多个 bot（`[[frontend]]`）；也支持 Lark 国际版（`domain` 切换）
- **双后端** —— Codex（thread/turn）与 Claude（session/conversation）并存，`/backend` 在线切换无需重启
- **会话与队列** —— 新消息排队、回复消息 steer 到当前 turn、失败自动回退、暂存附件、auto-retry
- **审批与表单** —— 命令 / 文件变更 / 权限审批，`request_user_input` 表单与手机友好的 quick-card
- **工作区管理** —— 多 workspace，支持克隆 / worktree / 删除 / 切换，群聊里每个 bot 在每个群可绑定不同工作区
- **Code Review / 技能**（Codex only）—— `/review` 多范围审查、`/skills` 技能调用
- **可观测性** —— token usage / context left、运行时日志级别切换与查看、飞书权限问题卡片化提示
- **运行与升级** —— Linux 用户态 systemd daemon、GitHub Release 自升级（`/upgrade`，按架构自动选型）

## 运行前提

### 必需

- 已安装 `codex` CLI，并且 `codex app-server` 可启动
- 一个可用的飞书应用 `app_id / app_secret`（也支持 Lark 国际版，设 `domain = "lark"`，详见[配置参考](docs/configuration.md#区域--region飞书-vs-lark)）

### 可选

- **Go 1.23+**（按 [go.mod](go.mod) 为准）：仅[从源码构建](#从源码构建)时需要；用[一键安装](#一键安装推荐)下载预编译二进制则不需要
- Linux + systemd 用户服务：如果你希望 Feidex 长驻后台并支持自升级，推荐使用 [daemon 模式](docs/operations.md#daemon-模式)

## 快速开始

### 一键安装（推荐）

下载对应平台的 release 二进制、校验 SHA-256、装到 PATH，无需 Go 工具链。

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/yuhuan417/feidex/main/install.sh | bash
```

```bash
# 装开发版 / 指定版本 / 自定义目录：
curl -fsSL https://raw.githubusercontent.com/yuhuan417/feidex/main/install.sh | bash -s -- --dev
curl -fsSL https://raw.githubusercontent.com/yuhuan417/feidex/main/install.sh | bash -s -- --version v0.222.0 --bin-dir /usr/local/bin
```

#### Windows（PowerShell）

```powershell
irm https://raw.githubusercontent.com/yuhuan417/feidex/main/install.ps1 | iex
```

> Windows 上 `feidex serve` 可直接运行，[daemon 模式](docs/operations.md#daemon-模式)也已支持（计划任务，免管理员）；仅 `/upgrade` 自升级为 Linux 专属。

安装选项见 [安装脚本](docs/operations.md#一键安装脚本)。

### 从源码构建

需要 Go 1.23+（按 [go.mod](go.mod) 为准）：

```bash
mkdir -p bin
go build -o bin/feidex ./cmd/feidex
```

### 配置并启动

```bash
# 准备配置（也可用 `feidex feishu setup` 走二维码授权；字段说明见「文档 → 配置参考」）
cp config.example.toml config.toml

# 启动
feidex serve --config config.toml
```

飞书应用的创建与绑定（`feidex feishu setup / new / bind`）见 [配置参考 · 飞书接入命令](docs/configuration.md#飞书接入命令)。

## 核心使用场景

### 群聊多 Bot

Feidex 支持一个进程同时跑多个飞书 bot。每个 bot 写成一个 `[[frontend]]`，可以指向不同后端、不同应用凭据和不同默认配置：

```toml
[[frontend]]
id = "pc-feidex"
backend = "codex"
app_id = "cli_xxx"
app_secret = "sec_xxx"
group_at_only = true

[[frontend]]
id = "pc-feiclaude"
backend = "claude"
app_id = "cli_yyy"
app_secret = "sec_yyy"
group_at_only = true
```

推荐在群里把不同 bot 绑定到不同 worktree：

```text
@pc-feidex /workspace
@pc-feidex /workspace new worktree

@pc-feiclaude /workspace
@pc-feiclaude /workspace new worktree
```

群聊路由规则：

- 明确 `@Bot` 的消息永远由被 `@` 的 bot 处理，适合配置 workspace、模型、权限和 worktree。
- 没有 `@` 的普通群消息只交给本群 primary bot；用 `@pc-feidex /primary on` 切换，用 `@pc-feidex /primary status` 查看。
- `primary` 只决定非 `@` 消息归谁，不会改变其他 bot 的群内 workspace 绑定。
- 群聊配置按 `frontend + chat` 隔离，所以同一个 bot 在不同群可以进入不同 worktree；不同 bot 在同一个群也可以各自绑定不同 worktree。
- 单聊也支持同一套 workspace / worktree 能力；单聊里不需要 primary，因为消息天然只发给当前 bot。

### Worktree 工作区

Worktree 适合让多个 bot 或多个群同时在同一个 Git 项目上工作，但各自拥有独立目录、分支和会话上下文。Feidex 提供两个入口：

```text
/workspace new worktree [BRANCH] [WORKSPACE_ID]
/workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]
```

- `/workspace new worktree` 基于当前已选 workspace 对应的本机 Git 仓库创建 worktree；也可以从菜单进入：`/workspace` -> `创建 Worktree`。
- `/workspace clone` 先 clone 一个 Git 仓库；在卡片里选择 `clone 后创建 worktree` 时，才会显示 worktree 字段。
- Worktree 表单里的 `基准工作区` 是本机已有 Git clone；`新分支` 是要创建的 branch；`workspace_id` 是 Feidex 里显示和切换用的 ID；`目录名` 是基准仓库同级目录下的新目录名。
- 能自动推导的字段都可以留空：默认用 bot 显示名和 base project 生成分支、workspace_id 和目录名，遇到重名会自动加数字后缀。
- 默认不会用 frontend id 或 chat id 生成给人看的名字，避免出现难读的目录名和分支名。

常见 Git 流程：worktree 会创建一个新的本地分支。开发完成后，通常执行 `git push -u origin <branch>` 推到远端，再发 PR 或合并到 `main`；不要把工作分支的 upstream 直接设成 `origin/main`。

## 文档

- [配置参考](docs/configuration.md) —— 最小配置、各配置块字段、飞书接入命令
- [使用指南](docs/usage.md) —— 会话语义、菜单与命令、Plan 模式、审批/表单/下载卡片、故障排查
- [运行与升级](docs/operations.md) —— daemon 模式、自动升级、发布流程
- [架构与开发](docs/architecture.md) —— 目录结构、架构视图、开发与测试
- [DEVELOPER.md](DEVELOPER.md) —— 开发契约：模块边界、构建产物规则、变更检查清单
- [Codex App Server 状态机审计](docs/codex-app-server-state-machine-audit.md) —— 协议状态机约束

## 文档口径

- 文档只描述“当前 release 的长期能力、当前行为和使用方式”，不承担按版本罗列增量的职责。
- 当前文档以 `v0.233.0` 的产品状态为准。
- 版本级新增能力、行为调整和修复记录见 GitHub Releases。
