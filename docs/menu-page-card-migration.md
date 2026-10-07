# 菜单页面卡迁移：方案与进度

状态：进行中（阶段 A / B 已完成，D 框架完成、页面迁移大部分完成，余量见下文清单）
关联契约：`DEVELOPER.md`（Interaction Constraints、High-Signal Test Guards、When Adding Or Changing A Menu Action）
守卫测试：`internal/feishuapp/menu_graph_guard_test.go`、`internal/application/features/registry_test.go`、`internal/feishuapp/menu_back_button_contract_test.go`

## 背景与目标

菜单卡片的跳转、返回、面包屑历史上靠各渲染器手写字符串串联，四个性质只有抽查式保障。本次改造把它们收敛为"声明树 + 图遍历守卫 + PageCard 构造注入"三层：

| 性质 | 保证手段 | 状态 |
| --- | --- | --- |
| ① 所有菜单能力可用 cmd 实现 | `registry_test.go` 强制每个可见菜单项携带 Slash 且能通过 `HandlesCommand` 路由；豁免：返回项、纯导航分组页 | 完成 |
| ② 所有卡片以 `/menu` 为根可达 | `menu_graph_guard_test.go` 从 `menu.root` BFS，遍历渲染出的真实卡片，声明节点未被渲染/不可达即失败 | 完成 |
| ③ 所有页面卡有返回上一级 | 守卫对每个节点页面断言"恰好一个 `返回上一级` 且在末位"；AST 扫描禁止 `返回` 字面量（扫描根已修正为 `internal/feishuapp` + `internal/adapter/feishu`） | 完成 |
| ④ 统一面包屑 | 守卫断言每个页面卡认领自己的声明面包屑路径；未声明路径的面包屑直接报错；PageCard 从声明树注入 | 完成 |

## 机制分层

1. **声明层** `internal/application/features`：`MenuNodes`（树 + Parent）、`MenuItems`（分组 listing + Slash）、`Commands`、`ActionNames` 是唯一事实源。面包屑（`menuutil.MenuCardBody*`）、返回目标（`features.MenuBackAction` = 节点 Parent）、分组按钮全部由此派生。`menu.*` 前缀保留给声明节点与菜单项；叶子操作按动词命名（如 `thread.new.start`、`review.start.base`）。
2. **守卫层** `menu_graph_guard_test.go`：group/p2p × codex/claude 四种 walk；分发按钮 value（带参数，支持 thread_id/workspace_id/index 等参数化页面）、模拟 select_static 选项（按 action+option 去重）、收割命令回复卡（命令桥接页的真实页面以新消息回复形式到达）。upgrade 三个节点延迟到遍历末尾（其异步检查会短暂使 codex gateway 不可用）。命令桥接页通过 `nodeSlash`（MenuItemSpec 的 Slash）直接走命令入口开页。
3. **构造层** `menuutil.PageCard` / `menuutil.MarkdownPageCard`：渲染器只提供节点、标题、正文、前向控件；面包屑与末位返回键由框架从声明树注入（`BackAction` 可覆盖、`BackParams` 携带返回附加上下文如页码）。运行时对未声明节点优雅回退到根路径，CI 由守卫兜底。

## 已迁移页面卡（走 PageCard / MarkdownPageCard）

- feishuapp：quiet 卡、群工作区管理卡（binding status）、fast 卡、群 Codex/Claude 模型卡、群辅助模型卡、status 面板
- adapter/feishu/modelconfig：p2p/全局 Codex 模型卡、Claude 模型卡、Codex 辅助卡、Claude 辅助卡（applyStatusTail 保留在返回键之后）
- adapter/feishu/history：Codex/Claude 历史列表卡、Codex/Claude Turn 详情卡（返回带 `page` 参数，经 `BackParams`）
- adapter/feishu/goalcmd：goal 状态卡、create/edit 表单卡、cleared 卡、replace 确认卡（goalButtons 不再自带返回键）
- adapter/feishu/debugviewcmd：Token Usage 卡、调试日志卡（面包屑从 div 文本提升为 markdown 头部）
- adapter/feishu/reviewcmd：review 菜单卡
- adapter/feishu/workspace：新建表单卡、clone 表单卡、worktree 表单卡、工作区管理卡（p2p）、删除列表卡、删除确认卡
- adapter/feishu/threadview：Codex/Claude 会话卡（并删除了包内重复的 menuBreadcrumbLabelsForBackend/menuNodeLabelForBackend/menuCardBodyForBackend 本地副本）
- adapter/feishu/backend（permission driver）：thread.sandbox.menu、thread.policy.menu、thread.multiagent.menu、thread.permission_mode.menu 四张线程设置页

## 迁移中发现并修复的真实缺陷

- thread 卡返回键是英文 `back` 且目标硬编码 → 改为 `feishu.MenuBackButtonText` + 声明树派生目标
- `workspace.new` 卡手写假面包屑 "主菜单 / workspace / new" → 改由声明树生成
- workspace new/clone/worktree 表单卡、goal 表单卡完全没有返回键 → 由 PageCard 注入
- 六个叶子操作挂着 `menu.*` 名字：`menu.new/fork` → `thread.new.start/fork.start`，`menu.review.base/commit/custom/uncommitted` → `review.start.*`
- `menu.goal` 缺 Node 声明；`workspace.binding.unbind`（操作）误声明为 Node（已撤销）
- back-button AST 扫描根仍指向已搬空的 `internal/app` → 改为渲染器实际所在目录
- 群工作区页返回键自环（menu.workspace→menu.workspace）→ 改为声明父级 menu.root（对应测试断言已更新）

## 有意保留手工构造的卡片（非节点页面）

- 根菜单卡（`menu.root`）：无返回键属预期（已是树根）
- 分组菜单卡：返回键来自声明中的 `MenuItemBack` 项，比构造注入更数据驱动
- compaction 三张状态卡、review 目标选择/失败卡、upgrade 运行卡、`renderBindingModelConfigOrErrorCard`/权限不足等错误卡：锚定操作卡或错误对话框，返回目标带请求上下文或锚定在父分组，不是独立节点页面；结构仍由图守卫校验
- `thread_feature_actions.go` 的中断占位/确认卡：瞬态卡

## 遗留与后续

- `upgradecmd`/`upgraderender` 的升级页共约 12 处面包屑仍手写：返回按钮携带请求上下文（prepare/confirm/cancel request id），需要 PageCard 支持多按钮尾部或单独迁移，收益有限（守卫已覆盖其结构正确性）
- 图守卫的命令回复收割依赖 fake client 同步回放；若未来命令回复改为纯异步 effect，需要给 walk 增加排空等待
- 新增菜单页面的工作流：在 `features/data.go` 声明 Node（+MenuItemSpec/Commands）→ 渲染器用 `menuutil.PageCard`/`MarkdownPageCard` 组装 → handler 注册到 action registry → `go test ./internal/feishuapp/ -run TestMenuGraphGuard` 验证

## 验证基线

- `go test ./...` 全绿；`TestMenuGraphGuard` 连续多次运行稳定（无 flake）
- 面包屑认领按"一行可对应多个声明节点"广播（如 `menu.group.model` 分区入口与 `menu.model` 页面渲染同一张卡），消除 map 迭代序导致的偶发失败
