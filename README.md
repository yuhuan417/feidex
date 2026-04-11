# Feidex

Feidex 是一个把 Codex App Server 接到飞书消息流上的中间层服务。

它的目标不是做一个通用聊天机器人，而是把「在本机/服务器上运行的 Codex 能力」以飞书对话的形式暴露出来，让你可以在飞书里：

- 发起 Codex 任务
- 在同一个 thread 里继续多轮交互
- 查看线程历史、状态、工作区、模型配置
- 审批命令/文件/权限请求
- 通过 daemon 模式做后台运行与自升级

项目面向中文使用场景，README 也以中文为主。

## 主要能力

- 飞书消息接入
  - 支持单聊与群聊
  - 群聊可配置为仅在 `@bot` 时响应
  - 支持回复树内的会话连续性
- Codex thread / turn 管理
  - 新消息默认 queue / 新 turn
  - 回复消息支持 steer 到当前 root 绑定的 turn
  - steer 失败时自动回退到 queue
  - 支持查看当前 thread 的 `/history`
- 菜单卡片
  - 单卡片内导航
  - 面包屑路径
  - 子菜单入口带展开标识
  - 真实命令项会显示对应 slash 命令
- 工作区管理
  - 多 workspace
  - workspace 级别 sandbox / approval policy
  - thread 级别 sandbox / policy / service tier
- 审批与补充输入
  - 命令审批
  - 文件变更审批
  - 权限审批
  - request_user_input / elicitation 表单
- 运行与升级
  - Linux 用户态 systemd daemon
  - GitHub Release 自升级
  - 自动按本机架构选择 `amd64` 或 `aarch64` 资产
- 发布
  - 自带打 tag 脚本
  - 可自动从 GitHub 远端 tag 推导下一个 minor 版本

## 目录结构

```text
cmd/feidex/                 主程序入口
cmd/feishu_card_demo/       飞书卡片 demo
internal/app/               核心业务逻辑（消息、菜单、审批、历史、steer）
internal/feishu/            飞书适配层
internal/codexrpc/          Codex App Server RPC 客户端与类型
internal/config/            配置与飞书绑定流程
internal/state/             本地状态存储（session/submission/message links）
internal/daemon/            daemon 安装、运行与升级
internal/release/           GitHub Release 查询与版本比较
scripts/create_github_release.sh  发布 tag 脚本
config.example.toml         配置样例
```

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
go build -o feidex ./cmd/feidex
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
group_at_only = true
respond_to_at_everyone = false
card_enabled = true
reply_in_thread = true
quiet = false

[codex]
command = "codex"
transport = "stdio"
experimental_api = true
service_name = "feidex"

[[workspace]]
id = "default"
name = "Default"
cwd = "."
model = "gpt-5.4"
approval_policy = "on-request"
sandbox_mode = "workspace-write"
```

### 3. 启动

```bash
./feidex serve --config config.toml
```

或者直接：

```bash
./feidex
```

## 飞书接入

Feidex 提供了三种飞书配置方式：

```bash
feidex feishu setup [--config config.toml]
feidex feishu new   [--config config.toml]
feidex feishu bind  --app app_id:app_secret
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

对应实现见：

- [feishu.go](/home/yuhuan/feidex/cmd/feidex/feishu.go)
- [feishu_setup.go](/home/yuhuan/feidex/internal/config/feishu_setup.go)

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
- `group_at_only`
  - 群聊是否仅在 `@bot` 时响应
- `respond_to_at_everyone`
  - 是否响应 `@所有人`
- `card_enabled`
  - 是否启用卡片输出
- `reply_in_thread`
  - 群聊是否在线程内回复
- `quiet`
  - Quiet 模式默认值

### `[codex]`

- `command`
  - 默认是 `codex`
- `transport`
  - `stdio` 或 `ws`
- `ws_url` / `ws_bearer_token`
  - 使用 websocket 连接远端 app-server 时启用
- `experimental_api`
  - 当前建议保持 `true`
- `service_name`
  - 提交到 Codex 的 service name
- `model`
  - 全局模型
- `reasoning_effort`
  - 全局推理强度

### `[[workspace]]`

支持多个 workspace，每个 workspace 可以有自己独立的：

- `cwd`
- `model`
- `approval_policy`
- `sandbox_mode`

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

### Staged Images

用户先发图片、后发文字时：

- 图片先进入暂存桶
- 下一条文字会把暂存图片一起带入输入
- 形成新 turn 成功后，会把所有参与本次输入的 root 绑定到该 turn

### `/history`

`/history` 不是从本地 submission 拼出来的，而是直接调用 `thread/read(includeTurns=true)` 读取 thread 历史，因此：

- 同一个 thread 上来自 Codex CLI / VSCode / app-server 的 turn 都能看见
- 当前展示重点是每个 turn 的输入和状态

## 菜单与命令

主菜单分五组：

- 常用工具
  - `中断任务 /stop`
  - `静默模式 /quiet`
  - `压缩上下文 /compact`
  - `下载文件 /download`
  - `历史记录 /history`
  - `Token 消耗 /usage`
- model
  - `模型配置 /model`
  - `响应速度 /fast`
- thread
  - `list` 下拉切换当前 workspace 的线程
  - `新建线程 /thread new`
  - `派生线程 /thread fork`
  - `配置线程沙箱 /thread sandbox`
  - `配置审批策略 /thread policy`
- workspace
  - `list` 下拉切换工作区
  - `新建工作区 /workspace new`
  - `配置默认沙箱 /workspace sandbox`
  - `配置默认策略 /workspace policy`
- system
  - `日志级别 /debug`
  - `查看日志 /debug logs`
  - `升级服务 /upgrade`
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
- `/debug on`
  - 切换到 debug 级别
- `/debug off`
  - 切换到 info 级别
- `/debug logs`
  - 查看最近一段服务端 slog 日志
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
- `/upgrade v0.3.0`
  - 跳过最新版本探测，直接发起指定版本升级确认

## 审批卡片

支持三类审批：

- 命令审批
- 文件变更审批
- 权限审批

审批处理后，卡片不会只剩“已允许本会话执行”这类结果文案，而会保留原审批内容，便于回看上下文。

## Daemon 模式

Feidex 支持 Linux 用户态 systemd daemon。

命令：

```bash
feidex daemon install --config config.toml
feidex daemon enable-linger
feidex daemon start
feidex daemon stop
feidex daemon restart
feidex daemon status
feidex daemon uninstall
```

说明：

- `install`
  - 安装并启动用户服务
- `enable-linger`
  - 为当前用户打开 linger，适合 SSH / 非登录会话
- `upgrade-runner`
  - 内部升级 runner，不需要手动调用

对应实现见：

- [daemon.go](/home/yuhuan/feidex/cmd/feidex/daemon.go)
- [manager.go](/home/yuhuan/feidex/internal/daemon/manager.go)

## 自动升级

自动升级要求：

- 当前运行在 Linux daemon 模式
- 服务进程正在运行
- 可访问 GitHub Release

升级逻辑会：

- `/upgrade`
  - 查询最新 release
- `/upgrade vX.Y.Z`
  - 直接查询指定 tag，跳过最新版本探测
- 根据本机架构选择正确二进制
  - `amd64 -> feidex-linux-amd64`
  - `arm64 -> feidex-linux-aarch64`
- 校验 `sha256sums.txt`
- 下载、替换、重启
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
- `sha256sums.txt`

## 开发与测试

### 常用测试

```bash
go test ./internal/app
go test ./internal/feishu
go test ./internal/state
go test ./internal/release
go test ./internal/daemon
```

如果当前环境的默认 Go cache 不可写，可以这样跑：

```bash
env GOCACHE=/tmp/feidex-gocache go test ./internal/app
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

说明当前不是 Linux daemon 场景，或者当前进程不是 daemon 服务进程。

### 3. 回复消息没有 steer

当前逻辑是：

- 只有“回复消息”才尝试 steer
- 如果 root 对应 turn 不可 steer，会自动回退 queue

所以看起来像“没 steer”，有可能其实是已经自动回退为新 turn。

### 4. 线程历史为空

`/history` 依赖 `thread/read(includeTurns=true)`。

如果当前 thread 本身没有 turn，或者 thread 还没正确加载，就可能看不到历史。

## 相关文件

- 配置样例：[config.example.toml](/home/yuhuan/feidex/config.example.toml)
- 发布脚本：[create_github_release.sh](/home/yuhuan/feidex/scripts/create_github_release.sh)
- 主入口：[main.go](/home/yuhuan/feidex/cmd/feidex/main.go)
- 飞书绑定：[feishu.go](/home/yuhuan/feidex/cmd/feidex/feishu.go)
- daemon 管理：[daemon.go](/home/yuhuan/feidex/cmd/feidex/daemon.go)
