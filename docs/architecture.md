# 架构与开发

本文给出代码结构与架构总览，便于快速定位模块。工程契约（仓库结构、构建产物规则、变更边界、协议约束）以 [DEVELOPER.md](../DEVELOPER.md) 为准；Codex App Server 协议状态机约束见 [docs/codex-app-server-state-machine-audit.md](codex-app-server-state-machine-audit.md)。

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
- app 物理子包的职责边界见 [docs/app-package-boundaries.md](app-package-boundaries.md)；新增子包不得反向 import `internal/app`。
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

Feidex 约定在“默认 Go cache 不可写”或“需要隔离 cache”时统一使用用户 cache 目录，避免 `/tmp` 在 tmpfs 机器上占用内存：

- `GOCACHE=${XDG_CACHE_HOME:-$HOME/.cache}/feidex/go-build`
- `GOMODCACHE=${XDG_CACHE_HOME:-$HOME/.cache}/feidex/gomodcache`

优先使用脚本，不要临时发明新的 `/tmp/go-build-*` 或 `/tmp/*-gomodcache` 路径。

等价环境变量写法：

```bash
env GOCACHE=${XDG_CACHE_HOME:-$HOME/.cache}/feidex/go-build GOMODCACHE=${XDG_CACHE_HOME:-$HOME/.cache}/feidex/gomodcache go test ./internal/app
```

清理：

```bash
./scripts/clean_tmp_go_cache.sh
```

清理脚本也会删除历史 `/tmp/feidex-gocache` 和 `/tmp/feidex-gomodcache` 目录。

### 版本信息

```bash
feidex version
```

构建时通过 `-ldflags` 注入：

- `feidex/internal/buildinfo.Version`
