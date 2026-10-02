# 架构与开发

本文给出代码结构与架构总览，便于快速定位模块。工程契约（仓库结构、构建产物规则、变更边界、协议约束）以 [DEVELOPER.md](../DEVELOPER.md) 为准；Codex App Server 协议状态机约束见 [docs/codex-app-server-state-machine-audit.md](codex-app-server-state-machine-audit.md)。长期目标架构见 [长期架构重构提案](architecture-refactor-proposal.md)。

## 目录结构

```text
cmd/feidex/                 主程序入口
cmd/feishu_card_demo/       飞书卡片 demo
internal/app/               应用协调层（frontend、session、菜单、审批、turn lifecycle）
internal/application/       用例层（统一输入、effect、routing 等）
internal/domain/             纯领域模型与状态转换
internal/adapter/            Feishu、backend 和 storage 适配器
internal/architecture/       依赖方向与分层架构测试
internal/app/appcore/       核心组合（client 接口、session key、workspace 选择）
internal/app/apphistory/    进程历史
internal/app/appstate/      应用状态 store
internal/adapter/feishu/approval/      审批卡片文案、按钮、文件摘要
internal/adapter/feishu/approvalview/  审批视图渲染
internal/app/autoretry/     自动重试状态
internal/app/backend/       后端驱动抽象（选择、action、failure、transition）
internal/adapter/feishu/cards/         飞书卡片构造 helper
internal/app/clauderuntime/ Claude 运行时集成
internal/app/claudesession/ Claude session 生命周期
internal/app/claudesupport/ Claude 诊断/历史 helper
internal/app/codexruntime/  Codex 运行时集成
internal/app/compact/       上下文压缩
internal/app/convbackend/   会话后端 facade
internal/adapter/feishu/delivery/      回复卡片分片、markdown 拆分
internal/application/features/         统一命令/菜单/action 注册
internal/app/feishuwrap/    飞书适配包装器
internal/app/finalcardpatch/最终卡片 patch 逻辑
internal/app/historycmd/    历史命令处理
internal/app/lifecycle/     pending request / lifecycle 共享谓词
internal/app/maintenance/   backend-agnostic 升级/维护 workflow
internal/app/modelconfig/   模型配置卡片与命令入口（迁移中的 adapter）
internal/domain/modelconfig/ 模型 scope resolution 与 turn snapshot 规则
internal/app/pathpick/      路径选择器
internal/adapter/feishu/pendingforms/  待处理表单
internal/app/replycontinuation/ 回复接续处理
internal/adapter/feishu/review/        review target 数据结构
internal/app/reviewcmd/     review 命令处理
internal/app/serverrequest/ 服务端请求处理
internal/domain/conversation/ 会话、backend lineage 与活动操作领域状态
internal/app/skills/        技能管理
internal/app/skillscmd/     技能命令处理
internal/app/submission/    submission 队列与生命周期
internal/app/threadmenu/    thread 菜单渲染
internal/adapter/feishu/threadview/    thread 视图渲染
internal/adapter/feishu/turn/          turn 管理
internal/runtime/turnbinding/          turn 绑定逻辑
internal/adapter/feishu/turnitem/      turn item 类型
internal/application/turn/ turn 生命周期协调
internal/app/turnstream/    turn 流处理
internal/app/upgradecmd/    升级命令处理
internal/app/upgraderender/ 升级卡片渲染
internal/application/presentation/usageview/ usage 视图渲染
internal/app/workspace/     workspace payload 与值对象
internal/app/workspacecmd/  workspace 命令处理
internal/feishu/            飞书适配层
internal/codexrpc/          Codex App Server RPC 客户端与类型
internal/claudecli/         Claude CLI stream-json 适配层
internal/config/            配置与飞书绑定流程
internal/state/             本地状态存储（session/submission/message links）
internal/textutil/          不依赖 app 的通用文本 helper
internal/daemon/            daemon 安装、运行与升级
internal/release/           GitHub Release 查询与版本比较
internal/codexinstall/      Codex CLI 安装与探测
internal/claudeinstall/     Claude CLI 安装与探测
scripts/create_github_release.sh  发布 tag 脚本
config.example.toml         配置样例
```

## 架构视图

当前兼容主路径是：飞书事件进入 `internal/feishu`，由 `internal/app` 转发到逐步迁移中的 application use case，再通过 backend adapter 调用 `internal/codexrpc` 或 `internal/claudecli`，运行时和可恢复状态写入 `internal/state`。长期目标路径见 [长期架构重构提案](architecture-refactor-proposal.md)。

关键边界：

- `frontend` 是运行时隔离边界；backend 选择、session lineage、pending request、message link 和运行时缓存都必须按 frontend 隔离。
- `internal/domain` 和 `internal/application` 拥有 backend-neutral 产品语义；Codex/Claude 协议细节应收敛在 backend adapter，避免散落到消息、菜单和审批编排里。`internal/app` 只保留迁移期间的入口、组合和协议敏感兼容编排。
- `internal/codexrpc` 只负责 Codex App Server 传输和协议类型；`internal/claudecli` 只负责 Claude CLI stream-json 协议。不要让它们理解飞书、session 或卡片。
- 命令与菜单通过 `internal/application/features/` 统一注册，按 backend 自动过滤可用命令。
- 依赖方向按长期架构提案和 architecture tests 执行；旧 app package boundary 仅作迁移定位。
- `internal/feishu` 只负责飞书 SDK、消息/卡片发送、文件分享、链接改写和权限问题转换；不要把业务策略放进适配层。
- 慢操作必须走"快速 callback ack → 异步执行 → patch card / follow-up"，尤其是 clone、review、upgrade、download 和外部网络请求。
- frontend 生命周期和异步任务准入由 `internal/runtime.FrontendRuntime` 持有；兼容入口仍可使用 `RunAsync`/`a.waitAsync()`，测试通过显式等待同步。
- 触碰 `internal/app`、`internal/codexrpc`、`internal/claudecli`、审批、turn/thread lifecycle、review、compaction、tool input 或 server request 时，要同步检查状态机审计文档。

### 迁移中的架构状态

- 现有 `internal/app` 已拆成多个子包，但仍存在 callback、宽 `App` interface 和 root glue。新的拆分以 [长期架构重构提案](architecture-refactor-proposal.md) 为准，优先把状态和用例迁移到 `internal/domain`、`internal/application`。
- conversation/session 类型、workspace 恢复校验、backend lineage 和活动操作转换已迁入 domain；删除 sessionctx、appcore session facade 和根活动操作转发文件。持久化字段和协议转换边界保持原有语义。
- submission 类型、状态枚举和 running/finalize 转换已迁入 `internal/domain/submission`；`internal/state` 保留 repository 实现，状态语义由 domain transition 持有。
- session queue 的 FIFO、去重和 active-operation 边界由 `internal/domain/conversation` 维护；repository 只保存队列快照。
- turn lifecycle 用例和 review/goal/compact 绑定规则位于 `internal/application/turn`；runtime binding 和 usage 采集位于 `internal/runtime/turnbinding`，`internal/app` 只提供 composition adapters。
- frontend 生命周期与 shutdown drain 已迁入 `internal/runtime`；submission startup 由纯领域转换和 runtime 串行协调器共同负责。
- 模型快照统一使用 `internal/domain/modelconfig.Snapshot`；Codex resume 字段解释位于 `internal/adapter/backend/codex`，已应用/待生效状态由 `internal/application/modelconfig` 计算。
- interaction reply/resolved 转换由 `internal/application/interaction` 协调，JSON store 适配器只映射 DTO；Codex 的 serverRequest/resolved 仍是权威终点。
- backend adapter 不再以 `internal/app/backend` 作为最终归属；Codex/Claude 协议转换应逐步移动到 `internal/adapter/backend`，application 只消费 backend-neutral event 和 gateway。
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
