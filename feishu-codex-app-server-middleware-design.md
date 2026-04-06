# Feishu 控制 Codex App Server 中间件详细设计（V1）

更新时间：2026-04-06

工作名：`feidex-gateway`

## 1. 文档目标

本文档定义一个独立中间件，用飞书机器人作为人机交互入口，驱动 `codex app-server` 完成线程管理、回合交互、审批、人机补充输入、进度展示和结果回传。

设计目标有四个：

1. 复用现有本地成熟实现中已经验证过的飞书接入模式，尤其是认证、长连接收消息、卡片回调、会话键和消息更新策略。
2. 不再走 `codex exec --json` 的一次性调用，而是直接对接官方 `codex app-server` 协议，完整承接 thread/turn/event/request 语义。
3. 在推荐部署模式下不依赖公网回调地址，尽量使用飞书 SDK 长连接接收事件和卡片回调。
4. 把 Codex 的多阶段交互压缩成飞书可承载的文本消息和卡片状态机，而不是只做“消息转 prompt”的薄转发。

### 1.1 新增产品约束

本版讨论新增以下产品约束，并直接纳入 V1 默认设计：

1. 必须支持 `amd64` 和 `arm64` 部署，且尽量减少系统级依赖。
2. `/` 必须能拉起命令菜单，优先服务手机端低输入成本。
3. 必须让用户清楚知道“哪条输入在运行、哪条在排队、哪条已完成”，且不能依赖高频消息刷屏。
4. 第一版必须先保证稳定可工作，再逐步叠加高级能力。
5. 必须保留 `feishu setup/new/bind` 这类飞书扫码接入能力。
6. 必须支持在中间件内选择和恢复 Codex thread，并尽可能支持对同机已存在 session 的冷接管。

## 2. 范围与非目标

### 2.1 V1 范围

- 飞书国内版 `Feishu` 优先，采用长连接接收消息事件。
- 支持单聊和群聊。
- 支持文本、图片、文件、音频输入。
- 支持 Codex 线程创建、恢复、切换、打断。
- 支持 Codex turn 流式进度展示。
- 支持命令执行审批、文件变更审批、权限审批。
- 支持 `tool/requestUserInput` 的按钮式或表单式回填。
- 支持飞书卡片即时更新、延时更新、消息 PATCH、消息删除。
- 支持会话持久化、线程绑定、待审批请求持久化、去重和恢复。

### 2.2 明确不在 V1 的能力

- 国际版 `Lark` 的专门适配。架构预留，但实现不是第一阶段重点。
- `dynamicTools` 和 `item/tool/call` 的客户端动态工具执行。
- 完整的 MCP elicitation 任意复杂表单渲染。V1 仅支持简单表单和 URL 跳转型。
- 完整的音频自动转写。V1 只做文件落地和路径提示。
- 多实例无状态横向扩缩容。V1 以单活实例或带粘性路由的部署为前提。

## 3. 设计基线与参考来源

### 3.1 本地代码基线

截至本文档编写时，已明确参考本地已有的飞书接入实现、卡片渲染实现和桥接协议实现。

这些内容提供了以下可直接借鉴的模式：

- 飞书长连接事件收取和回调分发。
- 机器人 open_id 获取、群聊 @ 过滤、消息去重、老消息抑制。
- `reply` / `create` / `patch` / `delete` 的消息发送策略。
- 基于 `session_key` 的会话路由。
- 卡片内 `value.session_key` 回传设计。
- 预览消息、进度卡片、按钮式审批交互。

### 3.2 官方文档基线

飞书官方文档：

- 事件概述：https://open.feishu.cn/document/server-docs/event-subscription-guide/overview
- 接收消息事件：https://open.feishu.cn/document/server-docs/im-v1/message/events/receive
- 发送消息：https://open.feishu.cn/document/server-docs/im-v1/message/create
- 回复消息：https://open.feishu.cn/document/server-docs/im-v1/message/reply
- 编辑消息：https://open.feishu.cn/document/server-docs/im-v1/message/update
- 更新已发送的消息卡片：https://open.feishu.cn/document/server-docs/im-v1/message-card/patch
- 撤回消息：https://open.feishu.cn/document/server-docs/im-v1/message/delete
- 处理卡片回调：https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks
- 配置卡片交互：https://open.feishu.cn/document/common-capabilities/message-card/add-card-interaction/interaction-module
- 延时更新消息卡片：https://open.feishu.cn/document/ukTMukTMukTM/uMDO1YjLzgTN24yM4UjN
- 上传文件：https://open.feishu.cn/document/server-docs/im-v1/file/create

OpenAI 官方文档：

- Codex App Server：https://developers.openai.com/codex/app-server/
- Codex Advanced Configuration：https://developers.openai.com/codex/config-advanced/

### 3.3 本地 Codex 协议基线

本文档同时锁定以下本地协议观察值：

- `codex-cli 0.118.0`
- `codex app-server generate-json-schema --out ...`
- `codex app-server generate-ts --out ...`
- `codex app-server --help`

原因是官方文档覆盖了主流程，但一些线上的精确字段和 CLI 传参仍以本地 schema 和帮助输出为准，尤其是：

- v2 请求/通知枚举
- `tool/requestUserInput`
- `item/permissions/requestApproval`
- `approvalsReviewer`
- WebSocket 监听和鉴权参数

## 4. 总体设计结论

### 4.1 推荐方案

推荐将中间件设计为一个长期运行的独立服务：

- 上游连接飞书开放平台，使用 Go SDK 长连接接收 `im.message.receive_v1` 和 `card.action.trigger`。
- 下游作为 `codex app-server` 的 JSON-RPC client。
- 默认通过 `stdio` 启动并托管 `codex app-server` 子进程，不直接暴露 app-server 端口。
- 通过本地嵌入式状态库持久化会话、线程映射、待处理审批和消息状态。

### 4.2 核心思路

这个系统的本质不是“聊天转发”，而是“两套状态机对接”：

- 飞书侧是 `消息 -> 卡片 -> 回调 -> 更新消息`
- Codex 侧是 `thread -> turn -> item -> server request -> response`

中间件负责做三件事：

1. 把飞书用户输入规范化为 Codex `thread/start` / `turn/start` / `turn/steer` 请求。
2. 把 Codex 的通知流渲染为飞书里的进度卡片、最终消息和交互卡片。
3. 把 Codex 的 server-initiated request 映射成飞书用户可点击、可提交、可追踪的审批和表单。

## 5. 架构与部署

## 5.1 逻辑架构

```text
+---------------------------+         +---------------------------+
| Feishu Open Platform      |         | Codex App Server          |
|                           |         |                           |
| - im.message.receive_v1   |         | - thread/*                |
| - card.action.trigger     |         | - turn/*                  |
| - message create/reply    |         | - item/*                  |
| - message patch/delete    |         | - requestApproval         |
+-------------+-------------+         | - requestUserInput        |
              |                       +-------------+-------------+
              | Feishu SDK / OpenAPI                |
              v                                     | JSON-RPC 2.0
+---------------------------------------------------+-----------+
|                 feidex-gateway                                 |
|                                                               |
|  Feishu Adapter                                               |
|  Session Router / Workspace Resolver                          |
|  App Server Client                                            |
|  Turn Event Aggregator                                        |
|  Card Renderer / Message Sender                               |
|  Approval Manager / User Input Manager                        |
|  State Store (Embedded KV / Optional SQLite)                  |
|  Observability / Retry / Supervisor                           |
+---------------------------------------------------------------+
```

## 5.0 实现语言与依赖原则

V1 实现层推荐如下：

- 主语言使用 Go。
- 中间件本身产出单二进制。
- 不依赖 Python、Node.js、Java 运行时。
- 不依赖系统级数据库服务。
- 除 `codex` CLI 本体外，不引入额外本地守护进程。

这样可以同时满足：

- `linux/amd64`
- `linux/arm64`
- 后续如有需要，也可兼容 `darwin/amd64` 和 `darwin/arm64`

部署依赖收敛后，整个系统的“外部前置”只剩两项：

1. 已安装对应架构版本的 `codex` CLI
2. 飞书应用凭证或扫码创建能力

## 5.2 推荐部署拓扑

### 拓扑 A：本机 Sidecar，`stdio` 托管，推荐

- 中间件和 `codex app-server` 在同一台机器。
- 中间件直接 `spawn("codex", ["app-server"])`。
- 双方通过 `stdin/stdout` 交换 JSON-RPC。
- 不暴露 app-server WebSocket。
- 最适合本地开发、单机服务、最小攻击面部署。

优点：

- 不需要额外的 app-server 端口和认证网关。
- 协议最简单，延迟最低。
- 进程生命周期可由中间件直接监管。

缺点：

- 中间件和 app-server 必须共机。
- 横向扩容时状态和进程管理更复杂。

### 拓扑 B：远程 `ws://` App Server，可选

- 中间件连接远程 `codex app-server --listen ws://IP:PORT`。
- 适用于中间件和执行节点分离的场景。

要求：

- 使用 app-server WebSocket 鉴权。
- 本地 `codex app-server --help` 显示支持：
  - `--ws-auth capability-token`
  - `--ws-auth signed-bearer-token`
  - `--ws-token-file`
  - `--ws-shared-secret-file`
  - `--ws-issuer`
  - `--ws-audience`
  - `--ws-max-clock-skew-seconds`

建议：

- 非 loopback 监听一律启用鉴权。
- 如果要跨主机部署，优先 `signed-bearer-token`。
- 生产环境再叠加 mTLS 或内网网关。

## 5.3 App Server 运行模式

中间件支持两种运行模式：

| 模式 | 说明 | 默认值 |
|---|---|---|
| `shared_instance` | 整个中间件共用一个 app-server 连接，多工作区靠 `cwd` 和 thread 维度区分 | `true` |
| `isolated_workspace` | 每个工作区单独一个 app-server 进程或连接 | `false` |

推荐默认用 `shared_instance`，因为官方协议已经支持：

- `thread/start` 指定 `cwd`
- `turn/start` 指定 `cwd`
- 每个线程单独持久化
- 多线程复用同一连接的通知流

当需要更强隔离时，再切换到 `isolated_workspace`。

## 5.4 飞书扫码接入能力

V1 必须提供 `setup/new/bind` 这一类扫码接入流程，建议直接提供以下命令面：

- `feidex feishu setup`
- `feidex feishu new`
- `feidex feishu bind`

行为要求：

- `setup` 为统一入口。
- 无凭证时走 `new`，终端打印二维码和 URL。
- 有 `app_id:app_secret` 时走 `bind`。
- 完成后自动写回本地配置文件。
- 尽量保留注释和排版，不粗暴重写整个配置。

这部分应保持用户体验稳定：统一入口、二维码输出、凭证绑定、以及自动写回配置。

## 6. 核心概念与数据模型

## 6.1 工作区（Workspace）

工作区是中间件面向业务的最小执行单元，包含：

- `workspace_id`
- `name`
- `cwd`
- 默认 `model`
- 默认 `approvalPolicy`
- 默认 `sandboxPolicy`
- 可选 `personality`
- 可选 `serviceName`
- 访问 ACL

一个飞书会话必须解析到一个工作区后，才能启动 Codex thread。

## 6.2 飞书会话（Feishu Session）

飞书会话是上层业务路由单元，不等于 Codex thread。它表示“飞书里一段连续可恢复的对话入口”。

建议的 `session_key` 设计如下：

| 场景 | `session_key` |
|---|---|
| 单聊 | `feishu:p2p:<chat_id>:<user_open_id>` |
| 群聊线程隔离 | `feishu:group:<chat_id>:root:<root_message_id>` |
| 群聊共享会话，非默认 | `feishu:group:<chat_id>` |

默认开启 `thread_isolation = true`，即群聊按根消息隔离会话。原因：

- 飞书群聊天然存在 thread/root 语义。
- Codex thread 与飞书 root thread 语义吻合。
- 可以避免同一群聊多个人混写同一上下文。

## 6.3 Codex Thread

Codex thread 是 app-server 原生会话对象，是实际的上下文持久化边界。

中间件维护：

- `session_key -> active_thread_id`
- 一个 `session_key` 在任意时刻只有一个 active thread
- 一个 `session_key` 可以历史关联多个 thread

## 6.3.1 Submission

V1 增加 `submission` 概念，用来解决“多条输入排队后看不清完成态”的产品问题。

定义：

- 一条飞书入站消息，映射为一个 `submission`
- 一个 `submission` 默认对应一个独立 Codex turn
- `submission` 可能处于 `queued`、`running`、`waiting_approval`、`waiting_user_input`、`completed`、`failed`、`interrupted`

关键决策：

- 默认不把“用户连续发的多条消息”自动合并为同一个 turn
- 默认走“串行队列”
- 只有用户显式选择“补充当前任务”时，才使用 `turn/steer`

## 6.4 Turn

Turn 表示一次用户请求及其后续生成过程。

中间件以 `turn_id` 作为运行期状态主键，记录：

- `turn_id`
- `thread_id`
- `status`
- `preview_message_id`
- `final_message_ids`
- `pending_request_ids`
- 增量缓冲区

## 6.5 Pending Server Request

当 app-server 发起 server-initiated request 时，中间件会持久化一个待处理请求：

- `request_id`
- `type`
- `thread_id`
- `turn_id`
- `item_id`
- `session_key`
- `owner_user_id`
- `message_id`
- `payload_json`
- `status`
- `expires_at`

支持的请求类型：

- `command_execution_approval`
- `file_change_approval`
- `permissions_approval`
- `tool_request_user_input`
- `mcp_elicitation`

## 7. 组件设计

## 7.1 Feishu Adapter

职责：

- 维护飞书长连接。
- 订阅并处理 `im.message.receive_v1`。
- 订阅并处理 `card.action.trigger`。
- 调用发送、回复、编辑、更新卡片、删除消息 API。
- 下载入站附件并上传出站附件。

建议沿用本地成熟实现中已经验证过的这些设计点：

- 启动时获取 bot open_id，用于群聊 `@bot` 过滤。
- 使用 `message_id` 去重。
- 忽略重启前的旧消息。
- 群聊默认只响应 @bot。
- 卡片按钮/选择器的 `value` 中携带 `session_key`。

V1 默认策略：

- 飞书使用长连接接收消息事件。
- 卡片回调也使用长连接接收。
- 不强制配置开发者服务器公网地址。

## 7.2 Session Router

职责：

- 根据消息上下文生成 `session_key`。
- 根据用户、群聊、工作区绑定规则解析 `workspace_id`。
- 查询和更新 `session_key -> active_thread_id`。
- 识别命令消息和普通用户输入。

路由顺序：

1. 如果消息是工作区切换命令，优先处理命令。
2. 否则读取显式 chat binding。
3. 再读取 user default workspace。
4. 再读取系统 default workspace。
5. 若仍无法解析，返回“请选择工作区”的卡片。

## 7.3 App Server Client

职责：

- 维护与 `codex app-server` 的单连接 JSON-RPC 传输。
- 完成 `initialize` + `initialized` 握手。
- 发送 client request。
- 接收 response、notification、server request。
- 对 request/response 做 request id 级别路由。

初始化时统一发送：

```json
{
  "method": "initialize",
  "id": 1,
  "params": {
    "clientInfo": {
      "name": "feidex",
      "title": "Feidex Feishu Middleware",
      "version": "0.1.0"
    },
    "capabilities": {
      "experimentalApi": true,
      "optOutNotificationMethods": [
        "item/reasoning/textDelta",
        "rawResponseItem/completed"
      ]
    }
  }
}
```

然后立即发送：

```json
{
  "method": "initialized",
  "params": {}
}
```

`experimentalApi = true` 是推荐默认值，因为 V1 要用到：

- `tool/requestUserInput`
- 更完整的审批字段
- 可能的 `additionalPermissions`

代价是协议变更风险更高，因此要加版本锁和 schema 测试。

## 7.4 Turn Event Aggregator

职责：

- 按 `thread_id + turn_id` 聚合 app-server 通知流。
- 把高频 item delta 压缩成飞书可显示的摘要。
- 维护 preview card 的渲染状态。
- 控制最终消息发送时机。

它不是简单透传，而是一个降噪层：

- `reasoning/textDelta` 默认不显示。
- `reasoning/summaryTextDelta` 只保留最近 N 段。
- `commandExecution/outputDelta` 只保留最后若干行。
- `fileChange/outputDelta` 只保留摘要和最近 diff 片段。
- `agentMessage/delta` 分 commentary 和 final_answer 处理。

## 7.5 Card Renderer

职责：

- 生成进度卡、审批卡、表单卡、线程列表卡。
- 将 `request_id`、`session_key`、`thread_id`、`turn_id` 等回路信息编码到卡片 `value` 中。
- 生成 Feishu 允许的共享卡或独享卡。

设计原则：

- 进度卡使用共享卡，`update_multi = true`。
- 审批动作默认共享展示，但只有授权用户能点击成功。
- 群聊中按钮可见不等于可执行，服务端必须做权限判定。
- 长文本不要塞进卡片正文，卡片只展示摘要，最终结果走普通消息。

## 7.6 Approval Manager

职责：

- 接收 app-server 的 server request。
- 渲染飞书卡片。
- 等待用户点击或表单提交。
- 把用户决策映射为 JSON-RPC response。
- 在 `serverRequest/resolved` 或 turn 结束时回收待处理请求。

## 7.7 Outbound Scheduler

新增 `Outbound Scheduler` 组件，专门负责飞书出站消息节流与合并。

职责：

- 对同一 `chat_id` 的发送和 PATCH 做令牌桶限流。
- 合并高频 delta，避免把模型流式输出直接映射成飞书消息风暴。
- 保证“每条 submission 最多一张状态卡 + 一组最终消息”。

限流策略设计：

- 按飞书官方限制，单用户和同群机器人共享为 `5 QPS`
- 中间件内部保守降档到：
  - 同 `chat_id` 稳态 `2 QPS`
  - 瞬时突发不超过 `4`

这样即便遇到网络重试、审批卡、最终结果分片，也不容易触达飞书侧限额。

## 7.8 State Store

V1 默认建议使用嵌入式纯 Go KV 存储，优先 `bbolt`。

原因：

- 零外部服务依赖。
- 无 CGO 依赖，跨 `amd64/arm64` 更稳。
- 单机单活场景足够。
- 启动和部署比 SQLite 更简单。

为什么不默认 SQLite：

- 虽然 SQLite 也可用，但如果选系统 SQLite 或 CGO 驱动，会增加跨架构部署复杂度。
- V1 的查询模式较简单，KV 足以覆盖。

保留演进方向：

- 存储层抽象为接口。
- 如后续需要更复杂的检索、统计或多实例协调，再加可选 SQLite 后端。

建议桶/集合：

| 桶/集合 | 用途 |
|---|---|
| `workspace` | 工作区配置 |
| `workspace_binding` | chat/user 到 workspace 的绑定 |
| `session_binding` | `session_key -> active_thread_id` |
| `thread_registry` | thread 历史索引 |
| `submission_queue` | 每个 session 的排队输入 |
| `turn_runtime` | 活跃 turn 运行态 |
| `pending_request` | 待审批/待补充输入 |
| `message_link` | 进度卡、最终消息、审批卡与 turn/request 的关联 |
| `inbound_dedup` | 飞书消息去重 |

## 8. 飞书侧接口与行为设计

## 8.1 必需权限

V1 建议申请的飞书权限：

| 权限 | 用途 |
|---|---|
| `im:message.p2p_msg:readonly` | 接收单聊消息 |
| `im:message.group_at_msg:readonly` 或 `im:message.group_msg:readonly` | 接收群消息 |
| `im:message:send_as_bot` 或 `im:message:send` | 机器人发送消息 |
| `im:message:readonly` | 读取消息内容、拉取消息详情 |
| `im:resource:upload` | 上传文件/图片/音频 |
| `contact:user.base:readonly` | 用户显示名缓存，可选但推荐 |

卡片相关：

- 启用机器人能力
- 订阅 `card.action.trigger`
- 配置卡片交互回调方式

## 8.2 入站消息处理

### 文本

1. 解析 `content` 的 JSON 字符串。
2. 去除 `@bot` 提及 token。
3. 如果内容恰好为 `/`，立即返回命令菜单卡。
4. 如果内容以 `/` 开头，进入命令分发器。
5. 否则进入普通对话路径。

### 图片

1. 根据 `message_id` 和资源 key 下载图片。
2. 写入 `session_attachment_dir`。
3. 将其转换为 app-server `localImage` 输入项。

### 文件

1. 下载到 `session_attachment_dir`。
2. 在 `turn/start.input` 中附加一条文本提示，例如：
   `User attached file: /data/attachments/.../design.pdf`
3. 同时在 turn 的 sandbox 可读目录中包含该 attachment root。

### 音频

V1 不做自动转写，处理方式与文件一致：

1. 下载音频文件。
2. 在文本提示中注明：
   `User attached audio file (not transcribed): /data/.../voice.opus`

后续如果要增强，可接入单独的 ASR 子模块。

### 群聊过滤

默认规则：

- 单聊全部接收。
- 群聊仅处理 `@bot` 消息。
- 若开启 `respond_to_at_everyone` 才处理 `@all`。

### 去重

飞书官方明确建议按 `message_id` 去重，不依赖 `event_id`。因此：

- `inbound_dedup` 主键以 `message_id` 为准。
- 卡片回调的幂等以 `request_id` / `pending_request` 状态为准。

## 8.3 出站消息策略

### 策略总览

| 场景 | API |
|---|---|
| 对触发消息直接回复 | `reply` |
| 主动发新消息 | `create` |
| 更新进度卡 | `patch` |
| 删除预览卡 | `delete` |
| 延时更新卡片 | `interactive/v1/card/update` |

### 回复还是创建

默认行为：

- 单聊：优先 `reply`
- 群聊线程隔离：优先 `reply`，并设置 `reply_in_thread = true`
- 无法 reply 时退化为 `create`

### 最终消息与进度卡分离

V1 推荐：

- 每条 `submission` 都有一张轻量状态卡
- 状态卡在 `queued/running/waiting/completed/failed` 之间切换
- 活跃 submission 的状态卡允许持续 PATCH
- turn 完成后发送最终文本/富文本消息
- 然后把该 submission 的状态卡更新为终态

不推荐把完整最终答案塞到卡片里，原因：

- 飞书卡片 30 KB 限制更紧
- 长文本 PATCH 风险高
- 代码块和长 diff 在卡片内可读性差

### 长文本分片

最终结果消息分片规则：

- 优先按段落和代码块边界切分
- 单条不超过安全阈值
- 如果超过飞书消息建议体积，拆成多条回复

### 已完成的隐式提示

V1 推荐同时做两层提示：

1. 每条 `submission` 自带状态卡，完成时自动变为绿色 `已完成`
2. 对原始用户消息加“处理中”轻量 reaction，完成后移除

这样用户不用主动看命令输出，也能从聊天列表里隐式感知任务是否结束。

### 飞书消息限额确认

根据飞书官方当前文档，关键限制如下：

- 向同一用户发送消息限频：`5 QPS`
- 向同一群组发送消息限频：群内机器人共享 `5 QPS`
- API 频率上限：`50 次/秒`、`1000 次/分钟`
- 文本消息体上限：`150 KB`
- 卡片/富文本消息体上限：`30 KB`

因此，V1 明确不采用“每个 delta 发一条消息”的策略，只允许：

- 初始状态卡 1 次
- 状态 PATCH 若干次，但经 Outbound Scheduler 合并
- 最终结果消息 1 组

在这个设计下，不会因为正常模型流式输出而打满飞书限额。

## 8.4 卡片交互

卡片按钮和表单提交的 `value` 至少包含：

```json
{
  "action": "approve.command.accept",
  "session_key": "feishu:group:oc_xxx:root:om_xxx",
  "thread_id": "thr_xxx",
  "turn_id": "turn_xxx",
  "request_id": "req_xxx"
}
```

这样即使消息上下文丢失，仍可从卡片本身恢复路由信息。

## 8.5 命令菜单

为降低手机端输入成本，V1 同时提供两种菜单入口：

### 入口 A：发送 `/`

- 用户只需发送单独一个 `/`
- 中间件立即回复一张命令菜单卡
- 菜单项包含：
  - `新会话`
  - `线程列表`
  - `中断当前任务`
  - `工作区切换`
  - `模型切换`
  - `补充当前任务`
  - `帮助`

### 入口 B：飞书 Bot Menu

V1 应提供飞书 bot menu 配置，并处理 `P2BotMenuV6` 事件：

- 打开命令菜单
- 新会话
- 线程列表
- 当前状态

这样即使用户不想输入 `/`，也能从机器人菜单直接打开控制面板。

## 9. Codex App Server 对接设计

## 9.1 传输协议

官方 app-server 文档说明：

- 使用双向 JSON-RPC 2.0 语义
- 线上的 `jsonrpc: "2.0"` 头省略
- `stdio` 模式为 JSONL
- `websocket` 模式为“每帧一条 JSON-RPC 消息”

因此中间件内部统一抽象：

```text
Transport
  - SendRequest(id, method, params)
  - SendNotification(method, params)
  - OnResponse(id, result/error)
  - OnServerRequest(id, method, params)
  - OnNotification(method, params)
```

## 9.2 初始化与连接生命周期

连接建立后固定顺序：

1. `initialize`
2. `initialized`
3. 后续才能发 `thread/start` / `thread/resume` / `turn/start`

任何初始化前请求都视为协议错误。

## 9.3 Thread 管理

### 新建线程

首次对话或 `/new` 时：

1. 调用 `thread/start`
2. 指定工作区默认 `model`、`cwd`、`approvalPolicy`、`sandbox`
3. 收到 `thread.id`
4. 写入 `session_binding.active_thread_id`
5. 再发 `turn/start`

### 恢复线程

用户执行 `/threads` 后，选择某个历史线程：

1. 调用 `thread/resume`
2. 把返回的 `thread.id` 设为 active
3. 后续正常 `turn/start`

### 线程列表

默认列表命令只展示中间件自己创建的线程：

- `sourceKinds = ["appServer"]`
- `cwd = workspace.cwd`
- 可选 `archived = false`

如果需要扩展为恢复 CLI/VSCode 线程，再增加 “全部线程” 视图。

## 9.4 Turn 管理

### 普通文本消息

当飞书普通消息进入时：

- 为该消息创建 `submission`
- 若 session 没有 active thread，执行 `thread/start` + `turn/start`
- 若 thread 存在且没有 active turn，执行 `turn/start`
- 若该 thread 正有 active turn，默认进入 `submission_queue`

默认排队而不是默认 `turn/steer`，原因：

- 用户更容易知道“哪条输入已经结束”
- 避免多条消息意外合并成一个 turn
- 更符合手机端连续发短句的使用习惯

### 显式补充当前任务

只有以下场景才使用 `turn/steer`：

- 用户点击命令菜单里的 `补充当前任务`
- 用户在状态卡中点击 `继续补充`
- 显式命令 `/append ...`

这样 `turn/steer` 变成用户可感知的高级动作，而不是默认行为。

### 打断

`/interrupt` 或卡片按钮触发：

1. 调用 `turn/interrupt`
2. 等待 `turn/completed(status = interrupted)`
3. 更新进度卡并释放 active turn

### Review 扩展

保留 `/review` 命令映射到 `review/start`：

- `/review uncommitted`
- `/review base main`
- `/review commit <sha>`
- `/review custom <instruction>`

这不影响核心聊天流程，但应在协议层预留。

## 10. 协议映射

## 10.1 飞书输入到 app-server 请求的映射

| 飞书输入 | 中间件动作 | app-server |
|---|---|---|
| 普通文本 | 创建 submission，默认串行排队 | `turn/start` |
| `/append ...` 或菜单“补充当前任务” | 补充当前活跃 turn | `turn/steer` |
| `/new` | 新线程 | `thread/start` |
| `/threads` | 线程列表 | `thread/list` |
| `/interrupt` | 打断当前 turn | `turn/interrupt` |
| `/review ...` | 启动 review | `review/start` |
| 图片 | 下载后作为本地图片 | `turn/start.input[{type:\"localImage\"}]` |
| 文件 | 下载后作为文本提示中的路径 | `turn/start.input[{type:\"text\"}]` |
| 音频 | 下载后作为文本提示中的路径 | `turn/start.input[{type:\"text\"}]` |

## 10.2 app-server 通知到飞书渲染的映射

| app-server 事件 | 飞书表现 |
|---|---|
| `thread/started` | 更新 session 绑定 |
| `turn/started` | 创建或刷新进度卡 |
| `item/started` | 更新当前阶段 |
| `item/agentMessage/delta` | 更新进度卡摘要 |
| `item/reasoning/summaryTextDelta` | 更新“思考摘要” |
| `item/commandExecution/outputDelta` | 更新“执行中命令”摘要 |
| `item/fileChange/outputDelta` | 更新“文件改动”摘要 |
| `turn/plan/updated` | 更新计划区块 |
| `serverRequest/resolved` | 将对应审批卡置为已处理 |
| `turn/completed` | 发送最终答案，结束进度卡 |
| `error` | 发送错误消息，进度卡置失败 |

## 10.3 server request 到飞书交互的映射

| server request | 飞书交互 |
|---|---|
| `item/commandExecution/requestApproval` | 命令审批卡 |
| `item/fileChange/requestApproval` | 文件变更审批卡 |
| `item/permissions/requestApproval` | 权限授权卡 |
| `item/tool/requestUserInput` | 选择题按钮卡或表单卡 |
| `mcpServer/elicitation/request` | 简单表单卡或 URL 跳转卡 |

## 11. 交互与状态机

## 11.1 Session 状态

| 状态 | 含义 |
|---|---|
| `idle` | 无 active turn |
| `queued` | submission 已入队，等待执行 |
| `turn_in_progress` | turn 正在执行 |
| `waiting_approval` | 正等待用户审批 |
| `waiting_user_input` | 正等待用户补充输入 |
| `interrupted` | turn 已被中断 |
| `error` | turn 或连接异常 |

## 11.2 状态迁移

| 事件 | 当前状态 | 下一个状态 |
|---|---|---|
| 收到普通消息且无 active turn | `idle` | `turn_in_progress` |
| 收到普通消息且有 active turn | `turn_in_progress` | `queued` |
| 收到 `requestApproval` | `turn_in_progress` | `waiting_approval` |
| 收到 `requestUserInput` | `turn_in_progress` | `waiting_user_input` |
| 用户提交审批/输入 | `waiting_*` | `turn_in_progress` |
| 收到 `turn/completed` | 任意活动态 | `idle` |
| 用户中断 | `turn_in_progress` | `interrupted` |
| 收到 `error` | 任意活动态 | `error` |

## 12. 详细场景流

## 12.1 场景 A：首次单聊提问

```text
用户 -> 飞书机器人: 帮我看一下这个仓库的测试失败原因
飞书 -> 中间件: im.message.receive_v1
中间件:
  1. 解析为单聊 session_key
  2. 解析 workspace
  3. 创建 submission
  4. thread/start
  5. turn/start
  6. 创建状态卡
app-server -> 中间件:
  turn/started
  item/*
  item/agentMessage/delta
  turn/completed
中间件 -> 飞书:
  patch 状态卡
  reply 最终答案
  patch 状态卡为完成态
```

## 12.2 场景 B：群聊中 @bot，在 thread 内连续追问

1. 群聊首条 @bot 消息到达。
2. 中间件生成：
   `feishu:group:<chat_id>:root:<message_id>`
3. 创建新 thread，并在 reply API 中设置 `reply_in_thread = true`。
4. 后续该 root thread 下的回复继续命中同一 `session_key`。
5. 如果上一 turn 未结束，新的文本消息默认进入排队，而不是自动 `turn/steer`。

这会把“飞书 root thread”稳定映射到“Codex thread”。

## 12.3 场景 C：命令执行审批

```text
app-server -> 中间件: item/commandExecution/requestApproval
中间件:
  1. 落 pending_request
  2. 发送审批卡
用户 -> 飞书卡片: 点击“临时允许”
飞书 -> 中间件: card.action.trigger
中间件:
  1. 校验点击者是否有权审批
  2. 回写 JSON-RPC response { decision: "accept" }
  3. 即时返回更新后的卡片，禁用按钮并显示“已提交”
app-server -> 中间件: serverRequest/resolved
app-server -> 中间件: item/completed / 后续 turn 继续
中间件 -> 飞书: patch 进度卡
```

审批按钮建议：

- `允许一次` -> `accept`
- `本会话允许` -> `acceptForSession`
- `加入规则` -> `acceptWithExecpolicyAmendment`
- `拒绝` -> `decline`
- `取消` -> `cancel`

如果是 managed network approval：

- 展示 `host`、`protocol`
- 不把 `command` 当成唯一可信展示文案
- 可额外提供 “永久允许 host” / “永久拒绝 host” 按钮

## 12.4 场景 D：文件变更审批

与命令审批类似，但 UI 更强调：

- 改动文件数
- 目标路径
- 可选 `grantRoot`

按钮：

- `允许一次`
- `本会话允许`
- `拒绝`
- `取消`

## 12.5 场景 E：request_user_input

当 app-server 发出 `item/tool/requestUserInput` 时：

- 如果是单题、选项不超过 3 个、且不需要自由输入：
  - 渲染为按钮卡
- 否则：
  - 渲染为表单卡

回答提交后回写：

```json
{
  "answers": {
    "question_id": {
      "answers": ["selected_value"]
    }
  }
}
```

### 密码/敏感输入策略

如果问题标记 `isSecret = true`：

- 单聊场景：允许表单收集
- 群聊场景：不在群卡片中采集，改为向操作者单独发起私聊表单

这样避免敏感信息出现在群回调上下文里。

## 12.6 场景 F：线程列表与恢复

用户发送 `/threads`：

1. 中间件调用 `thread/list`，过滤当前工作区和 `sourceKinds=["appServer"]`
2. 返回线程列表卡
3. 每一项按钮包含 `thread_id`
4. 用户点击后执行 `thread/resume`
5. 更新 `session_binding.active_thread_id`
6. 返回“已切换到线程 xxx”提示卡

## 12.7 场景 G：进度展示

每条 `submission` 都有自己的状态卡，建议包含：

- 标题：`运行中` / `等待审批` / `等待输入` / `已完成` / `失败`
- submission 序号
- 原始用户输入摘要
- 工作区、模型、thread id 简写
- 最近一段 reasoning summary
- 当前命令或最近工具动作
- 文件改动数量
- 快捷按钮：`中断`、`新会话`、`线程列表`、`补充当前任务`

渲染限制：

- reasoning 摘要最多保留最近 3 段
- 命令输出只保留最后 20 行或最后 1200 字符
- 卡片总 payload 控制在安全阈值内，超过时自动截断并附加 `...`

队列展示规则：

- 同一 session 最多展示最近若干条 `queued/running` submission
- 已完成 submission 卡可折叠为短状态
- 如果队列变长，使用一张总览卡显示：
  - `运行中 1`
  - `排队中 N`
  - 最近完成 1 条

## 13. 命令面设计

建议支持以下文本命令：

| 命令 | 作用 |
|---|---|
| `/` | 打开命令菜单 |
| `/new` | 新建线程并切换为当前会话 |
| `/threads` | 查看当前工作区最近线程 |
| `/threads all` | 尝试列出同机可接管线程 |
| `/interrupt` | 打断当前 turn |
| `/append <text>` | 补充当前活跃任务，映射到 `turn/steer` |
| `/status` | 查看当前会话状态 |
| `/workspace list` | 列出可用工作区 |
| `/workspace use <id>` | 切换当前会话工作区 |
| `/model list` | 拉取模型列表 |
| `/model set <id>` | 设置后续 turn 默认模型 |
| `/review ...` | 启动 review |

这些命令不直接暴露底层 JSON-RPC 细节，但一一映射到底层方法。

## 14. 安全设计

## 14.1 飞书侧

- 推荐长连接模式，减少公网入口。
- 如果需要 webhook 模式，必须开启签名校验与加密校验。
- 群聊默认只响应 @bot。
- 群聊审批默认只允许会话发起人点击成功。

## 14.2 Codex 侧

- `sandboxPolicy` 由工作区配置控制，不允许由用户自由拼装。
- 默认审批策略建议为 `on-request` 或等价安全策略，不建议默认 `never`。
- 如果使用远程 WebSocket app-server，必须开启 ws auth。

## 14.3 中间件侧

- `pending_request` 必须带 `owner_user_id` 和 ACL 判定。
- 所有审批决策写审计日志。
- Secret 类表单答案不写入普通日志和状态库明文表。
- attachment 目录按 session 分隔，并设置过期清理。

## 15. 幂等、重试与恢复

## 15.1 飞书去重

- 消息按 `message_id` 去重。
- 卡片点击按 `request_id + action` 去重。

## 15.2 Feishu API 重试

出现以下情况时重试：

- access token 失效
- 短时限流
- 短暂网络错误

不重试：

- 参数错误
- 权限错误
- 卡片 JSON 非法

## 15.3 App Server 恢复

### `stdio` 子进程退出

1. supervisor 重新拉起 app-server
2. 重新 `initialize`
3. 当前 in-memory 订阅全部失效
4. active turn 标为 `error`
5. 向对应飞书 session 发一条“执行服务已重启，请重试或 /threads 恢复”的提示

### 中间件重启

1. 恢复 SQLite 中的 `session_binding`
2. 不自动恢复所有 thread 到内存
3. 用户下一条消息到来时，再懒恢复 `thread/resume`
4. 所有旧的 `pending_request` 标记为 `expired`

## 15.4 卡片更新竞争

每个 preview card 维护 `render_version`：

- 只有更高版本更新可以 PATCH
- 过时的异步更新直接丢弃

这样避免“旧状态覆盖新状态”。

## 16. 数据库建议结构

### `session_binding`

| 字段 | 说明 |
|---|---|
| `session_key` | 主键 |
| `workspace_id` | 当前工作区 |
| `active_thread_id` | 当前活跃 thread |
| `owner_user_id` | 会话发起人 |
| `chat_id` | 飞书 chat id |
| `chat_type` | p2p/group |
| `root_message_id` | 群 thread 根消息 |
| `status` | idle/active/waiting/error |
| `updated_at` | 更新时间 |

### `pending_request`

| 字段 | 说明 |
|---|---|
| `request_id` | app-server request id |
| `type` | 请求类型 |
| `thread_id` | 所属 thread |
| `turn_id` | 所属 turn |
| `item_id` | 所属 item |
| `session_key` | 所属飞书会话 |
| `owner_user_id` | 允许操作的人 |
| `feishu_message_id` | 对应审批卡消息 id |
| `payload_json` | 原始请求内容 |
| `status` | pending/resolved/expired |
| `expires_at` | 超时时间 |

## 17. 观测与运维

## 17.1 日志字段

建议统一结构化日志字段：

- `session_key`
- `workspace_id`
- `chat_id`
- `user_id`
- `thread_id`
- `turn_id`
- `item_id`
- `request_id`
- `feishu_message_id`
- `event_type`

## 17.2 关键指标

- `feishu_inbound_total`
- `feishu_outbound_total`
- `feishu_card_callback_total`
- `appserver_request_latency_ms`
- `turn_duration_ms`
- `approval_pending_count`
- `approval_decision_total`
- `session_active_count`
- `transport_reconnect_total`
- `message_patch_fail_total`

如果配置了 `serviceName`，app-server 还会给 Codex 自身指标打该服务名标签。

## 17.3 健康检查

建议提供：

- `/healthz`：进程活着
- `/readyz`：飞书连接正常，app-server initialize 完成
- `/diagz`：SQLite、workspace 配置、当前连接状态

## 18. V1 默认行为决策

为减少后续讨论成本，本文直接给出推荐默认值：

| 配置项 | 默认值 | 原因 |
|---|---|---|
| 飞书消息模式 | 长连接 | 不依赖公网 |
| 卡片回调模式 | 长连接 | 与消息模式一致 |
| 群聊是否必须 @bot | `true` | 降噪 |
| 群聊是否线程隔离 | `true` | 贴合 Codex thread |
| app-server 传输 | `stdio` | 最小攻击面 |
| 是否启用 `experimentalApi` | `true` | 需要 request_user_input 等能力 |
| 进度展示 | 共享卡 + 最终文本分离 | 飞书卡片容量更稳 |
| 审批人策略 | 单聊=发送者；群聊=会话发起人 | 降低误触风险 |
| 线程列表过滤 | `sourceKinds=["appServer"]` | 避免污染本地 CLI 线程 |

## 19. 分阶段落地计划

## Phase 1：最小可用链路

- 飞书长连接收文本
- `thread/start` / `turn/start`
- 最终文本回复
- 单聊场景

## Phase 2：完整对话体验

- 群聊 + thread isolation
- 进度卡 PATCH
- `/new` `/threads` `/interrupt`
- SQLite 持久化

## Phase 3：审批闭环

- 命令执行审批
- 文件变更审批
- `serverRequest/resolved`
- 审计日志

## Phase 4：高级表单与附件

- `tool/requestUserInput`
- 图片输入
- 文件下载与路径注入
- 工作区切换

## Phase 5：增强能力

- `permissions/requestApproval`
- MCP elicitation 简单表单
- 远程 WebSocket app-server
- Review 命令

## 20. 主要风险与对应策略

| 风险 | 影响 | 策略 |
|---|---|---|
| `experimentalApi` 演进 | 字段变化导致兼容性问题 | 锁定 `codex-cli` 版本，加入 schema 回归测试 |
| 飞书卡片 30 KB 限制 | PATCH 失败 | 卡片只展示摘要，长文本单独发消息 |
| 群聊多人同时点击审批 | 风险操作被误批准 | 服务端强校验 `owner_user_id` / ACL |
| 进程重启丢失 thread 订阅 | turn 中断 | 重启后懒恢复 thread，旧 turn 标错误并提示用户 |
| 远程 ws 暴露 | 安全风险 | V1 默认禁用，必要时必须启用 ws auth |
| 文件/音频输入不被原生支持 | 体验不一致 | 下载后转为路径提示，后续再补转写/文件理解增强 |

## 21. 最终结论

这个中间件应该被实现成“飞书适配器 + Codex app-server client + 状态机 + 持久化”的完整服务，而不是一个 webhook shell。

推荐首版采用以下组合：

- 飞书长连接
- `codex app-server` 本机 `stdio` 托管
- 群聊 thread 隔离
- 共享进度卡 + 最终文本回复
- SQLite 状态持久化
- `experimentalApi = true`

这样可以同时满足：

- 没有公网依赖
- 能承接 Codex 官方 thread/turn/approval 语义
- 交互上延续现有飞书机器人体验
- 后续能平滑扩展到 remote ws、工作区隔离、review、MCP 等能力
