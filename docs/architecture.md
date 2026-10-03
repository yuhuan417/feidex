# 架构与开发

本文是当前代码基线的导航文档。工程契约（仓库结构、构建产物、缓存和变更边界）以 [DEVELOPER.md](../DEVELOPER.md) 为准；Codex App Server 的协议约束见 [状态机审计](codex-app-server-state-machine-audit.md)；最终目标架构和完成判定见 [长期架构目标与边界](architecture-refactor-proposal.md)。

## 目录结构

```text
cmd/feidex/                         CLI 入口
cmd/feishu_card_demo/               卡片 demo
internal/domain/                    纯领域模型、状态和不变量
internal/application/               用例、输入、语义 effects 和 presentation
internal/adapter/backend/           Codex/Claude 协议与 backend 能力适配
internal/adapter/feishu/            Feishu 事件、卡片、渲染和 outbound 适配
internal/adapter/storage/           JSON/scoped repository 适配
internal/runtime/                   frontend 生命周期、session actor、进程、恢复和 effects
internal/composition/               frontend/runtime/application 构造根
internal/app/                       仅 Feishu callback 的薄入口
internal/feishuapp/                 Feishu frontend 实现（由 composition 构造）
internal/state/                     本地持久化 Store 与 runtime state gateway
internal/feishu/                    SDK transport、权限和平台事件声明
internal/codexrpc/                  Codex App Server transport/types
internal/claudecli/                 Claude CLI stream-json transport/types
internal/config/                    配置解析和配置文件语义
internal/architecture/              依赖与职责架构测试
```

## 依赖与 owner

- `internal/domain` 只依赖标准库和领域内部包；不依赖 adapter、storage、runtime、config 或 `App`。
- `internal/application` 只消费 domain 和 consumer-owned ports，产出 typed input、semantic effects 与 detached presentation；不持有 Feishu SDK、backend wire DTO 或 `*App`。
- `internal/adapter` 将外部协议转换为 application 值并执行 effects；Feishu adapter 不导出业务状态 owner，backend adapter 不把产品规则放回协议层。
- `internal/runtime` 持有每个 frontend 的 `FrontendOwner`、生命周期 cancellation、`SessionActors`、backend process/client、recovery、retry 和 effect deduper。相同 session 的状态转换在 actor 内串行，不同 session/frontend 可并行且互相隔离。
- `internal/composition` 负责构造和注入 owner 以及 `internal/feishuapp` frontend。`internal/app` 只负责 Feishu callback binding，不新增 God interface、service locator 或 alias shim。
- `internal/state.Store` 负责锁、clone、normalize、持久化和 scoped repository 操作，不执行网络、卡片、RPC 或进程操作。

## 关键控制流

Feishu message/card、backend notification/request、recall/reaction 和 retry timer 都先转换为 `internal/application` 的 typed input，再由 dispatcher 按 `application.SessionActorKey` 进入当前 frontend 的 session actor。application use case 先完成状态转换并发布 semantic effects；`internal/runtime` 的 effect runner 在执行外部 effect 前处理 cancellation、frontend scope、保存顺序和幂等；Feishu/backend adapter 最终执行 transport。

慢操作遵循“快速 callback ack → 异步工作 → card patch/follow-up”。卡片 callback、异步 user input、pending replay、队列续跑和 turn completion 不能绕过 session actor 修改 session/submission/turn；application 通过 `RunSessionAsync(sessionKey, fn)` 表达这个边界。Frontend-wide 的 startup、transport recovery 和 maintenance 可以使用 frontend runner，但其 session 队列恢复必须重新投递到对应 actor。

## 协议和生命周期边界

涉及 thread、turn、steer、approval、server request、review、compaction、goal continuation 或 tool input 的修改，必须对照 [Codex App Server 状态机审计](codex-app-server-state-machine-audit.md)，并保持 `turn/completed` 终态、`serverRequest/resolved` 权威边界、steer 的 `expectedTurnId` 和 frontend/session scope。真实 token 的 live integration tests 不属于默认验证。

## 验证

常规验证使用工程脚本和 Feidex cache：

```bash
./scripts/with_tmp_go_cache.sh go test ./...
./scripts/with_tmp_go_cache.sh go test -race ./...
./scripts/with_tmp_go_cache.sh go vet ./...
git diff --check
```

架构守卫位于 `internal/architecture/architecture_test.go`，并覆盖分层 import、`App` 能力泄漏、backend wire 组装、semantic effects、presentation、session actor 异步入口、state repository 外部 effect 和 Feishu adapter outbound 边界。`SaveState` 必须先于后续 effects，保存失败不得发布后续副作用。
