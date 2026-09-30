# 飞书权限与事件订阅清单

> 更新时间:2026-09-30
>
> 适用范围:Feidex 当前代码中所有调用飞书 API、处理飞书事件的功能点(`internal/feishu`、`internal/app`)。
>
> 核对口径:
> - 权限 ID 与"开启任一即可"的组合关系按飞书开放平台文档整理。
> - 功能与权限 / 事件的对应关系由本仓库代码推导;运行时的实际校准以代码里的清单为准(见第一节「启动自愈」)。
> - 某个具体部署实际申请了哪些权限、订阅了哪些事件,属于运行时配置,不在本文档记录;核对方法见末节。

## 一、四个配置层,以及"发布才生效"

飞书应用配置分四层,任何一层改动都必须**创建版本并发布**后才生效:

| 层 | 后台位置 | 决定什么 |
| --- | --- | --- |
| 应用能力 | 添加应用能力 | 机器人、消息卡片(卡片菜单/审批依赖) |
| 权限 scopes | 权限管理 | 能调用哪些服务端 API(缺权限报 99991672 等) |
| 事件订阅 | 事件与回调 → 事件配置 | 平台向 Feidex 推送哪些事件(缺订阅则 handler 永远不会触发) |
| 回调 | 事件与回调 → 回调配置 | 卡片按钮/表单回调(`card.action.trigger`) |

> 提醒:**"权限管理里勾上了" ≠ "线上已生效"** —— 只申请不发布时,线上会持续报缺少权限的错误;四层配置的改动都必须随版本发布。

### 订阅纪律(编码规则)

**代码中注册的每个事件 handler 都必须真实处理该事件;不使用的事件不得注册 handler。代码的 handler 集合就是产品需要订阅的事件清单。**

- handler 与订阅成对增删:先落地 handler(含测试),再在后台为该机器人订阅;handler 删除后,同步取消后台订阅。
- 只为消除 SDK 噪音而注册的 no-op handler 不算处理,属于违规写法;正确做法是取消对应的事件订阅,而不是用空 handler 掩盖。
- 某个机器人是否申请了某项权限、是否订阅了某个事件,属于运行时配置,不在本文档记录;发布版本前可按末节的方法核对该部署的 `event_infos` 是否与代码 handler 集合一一对应。

### 启动自愈

机器人**每次启动时会自动校准**飞书侧配置:把「二进制内声明的需求清单」与「线上实际生效的配置」对比,发现漂移即自动修复。

- **需求清单自包含在代码里**,运行时不读取任何外部文档:
  - 权限:`internal/feishu/appconfig/requirements.go` 的 `RequiredScopes()`(按功能分组,含注释);
  - 事件:由 `internal/feishu/events.go` 的 handler 注册表直接导出(`RequiredEventTypes()`)——订阅清单与 handler 同源,结构上不可能漂移。
- **行为**:发现漂移且已具备 `application:application:patch` → 自动改配置、自动发布新版本,并发一张结果卡片;缺 patch → 只发一张带申请链接的卡片(标题「需要飞书授权」),授权后下次启动自动完成剩余配置;配置一致 → 静默(仅日志)。
- **严格同步**:代码清单之外的事件订阅会被自动移除——在后台手工临时加的实验性订阅会在机器人下次启动时被清掉,这是预期行为。
- **新增功能时只需更新代码里的清单**(通常就是注册一个 handler + 在 `RequiredScopes()` 里补 scope),本节的表格仅作人读参考。

## 二、功能 → 权限映射

### 2.1 消息收发(核心链路)

| 功能 | API / 事件 | 所需权限 | 说明 |
| --- | --- | --- | --- |
| 接收单聊消息 | `im.message.receive_v1` | `im:message.p2p_msg:readonly` | |
| 接收群聊 @机器人 消息 | `im.message.receive_v1` | `im:message.group_at_msg:readonly` | |
| 接收群聊全部消息 | `im.message.receive_v1` | `im:message.group_msg`(敏感) | 非 @ 消息也会推送;是否响应由 Feidex 自身的 group message policy 决定 |
| 发送 / 回复消息与卡片 | `POST /im/v1/messages`、`POST /im/v1/messages/:id/reply` | `im:message:send_as_bot` | |
| 流式更新卡片 | `PATCH /im/v1/messages/:id` | `im:message:send_as_bot` 或 `im:message` | 卡片需声明 `update_multi`;仅限未撤回、14 天内的卡片 |
| 读取消息内容 | `GET /im/v1/messages/:id` | `im:message` 或 `im:message:readonly` | 回复/引用上下文 |
| 上传图片/文件到消息 | `POST /im/v1/images`、`POST /im/v1/files` | `im:resource` | |
| 下载消息中的资源 | `GET /im/v1/messages/:id/resources/:file_id` | `im:resource` | 用户发来的图片/附件 |
| 表情回执(排队 / THINKING / 👎) | reactions create / delete | `im:message.reactions:write_only` | 👎(ThumbsDown)同时是"取消排队 / 丢弃暂存输入"入口 |

### 2.2 群聊能力

| 功能 | API / 事件 | 所需权限 | 说明 |
| --- | --- | --- | --- |
| 群信息(群名等) | `GET /im/v1/chats/:chat_id` | `im:chat:readonly` 或 `im:chat:read` 或 `im:chat` | |
| 机器人进群注册(onboarding 卡片) | `im.chat.member.bot.added_v1` | 无 scope,需订阅事件 | 群工作区引导卡片 |
| 群公告状态区(多 Bot 状态) | docx chat announcement block list / create / batch update | `im:chat.announcement:read` + `im:chat.announcement:write_only` | 读取用 read,写入用 write_only;注意频控 99991400 |

### 2.3 本地文件链接(云空间)

| 功能 | API | 所需权限 | 说明 |
| --- | --- | --- | --- |
| 上传本地文件生成预览链接 | drive `upload_prepare` / `upload_part` / `upload_finish` | `drive:drive` 或 `drive:file` 或 `drive:file:upload` | |
| 文件夹管理 + 旧文件清理 | drive `file/create_folder` / `file/list` / `file/delete` | `drive:drive`(推荐);其中 `file/list` 也可用 `drive:drive:readonly` 或 `space:document:retrieve` | 清理(artifact gc)走 list + delete |
| 文件元数据批量查询 | drive `meta/batch_query` | `drive:drive.metadata:readonly`(或 `drive:drive`) | |
| 预览文档协作者授权 | drive `permission/members/create` | `docs:permission.member:create` | 把群/用户加为预览文件协作者 |

### 2.4 通知与加急

| 功能 | API | 所需权限 | 说明 |
| --- | --- | --- | --- |
| 权限错误通知卡片 + 加急 | `PATCH /im/v1/messages/:id/urgent_app` | `im:message.urgent` 或 `im:message.urgent:app_send` | 缺权限时调用失败;代码对此仅记录 WARN 降级,通知卡片照常发送,只是不会加急 |

### 2.5 卡片交互

| 功能 | 事件 / 回调 | 所需权限 |
| --- | --- | --- |
| 审批卡片按钮、菜单卡片、表单提交 | `card.action.trigger`(回调配置)+ 应用能力"消息卡片" | 无 scope |

## 三、事件订阅清单

按订阅纪律(第一节),下表是代码中注册了真实 handler 的全部事件;部署的后台订阅应从这份清单中选取并与之对应,标「可选」的按需订阅。

| 事件 | 用途 | 代码 handler | 必需性 |
| --- | --- | --- | --- |
| `im.message.receive_v1` | 接收消息,一切对话的入口 | `OnP2MessageReceiveV1` → `HandleFeishuMessage` | **必需** |
| `im.message.reaction.created_v1` | 👎 取消排队 / 丢弃暂存输入 | `OnP2MessageReactionCreatedV1` → `HandleFeishuReaction` | **必需**(要取消排队) |
| `card.action.trigger`(回调) | 卡片按钮/表单交互 | `OnP2CardActionTrigger` → `HandleCardAction` | **必需** |
| `im.chat.member.bot.added_v1` | 机器人进群 onboarding 卡片 | `OnP2ChatMemberBotAddedV1` → `SetBotGroupAddedHandler` | 群聊 onboarding 依赖 |
| `im.message.recalled_v1` | 撤回取消排队、清理撤回的暂存图片 | `OnP2MessageRecalledV1` → `HandleFeishuRecall` | 可选(要用才订阅) |

## 四、新建应用的最小权限集

**基础(缺一不可):**

- 能力:**机器人** + **消息卡片**
- 权限:`im:message:send_as_bot`、`im:message:readonly`(或 `im:message`)、`im:resource`、`im:message.reactions:write_only`、`im:chat:readonly`
- 接收:`im:message.p2p_msg:readonly`(单聊)、`im:message.group_at_msg:readonly`(群聊 @)
- 事件订阅:`im.message.receive_v1`、`im.message.reaction.created_v1`
- 回调配置:`card.action.trigger`
- 订阅方式:**长连接 / persistent connection**(无需公网回调 URL)

**按功能追加:**

| 功能 | 追加配置 |
| --- | --- |
| 群聊接收全部消息(非 @) | `im:message.group_msg`(敏感) |
| 群 onboarding / 群公告状态区 | 事件 `im.chat.member.bot.added_v1`;权限 `im:chat.announcement:read` + `im:chat.announcement:write_only` |
| 本地文件链接(云空间) | `drive:drive`、`drive:drive.metadata:readonly`、`docs:permission.member:create` |
| 加急提醒 | `im:message.urgent` |
| 撤回取消排队 | 事件 `im.message.recalled_v1` |

**不需要**(Feidex 代码不使用):`im:message:recall`、`im:message.reactions:read`、`im:message:update`(patch 用 `send_as_bot` 即可)、`im:chat:update`、`contact:*`(Feidex 全程使用 open_id)。

## 五、自查与排错

- **后台自查**:开发者后台 → 应用 → 权限管理 / 事件与回调 / 版本管理与发布。
- **命令行自查**(核对某个部署的实际状态):先取应用信息接口的 `online_version_id`,再调
  `GET /open-apis/application/v6/applications/{app_id}/app_versions/{version_id}`,返回的 `scopes`、`events`、`event_infos` 就是该版本**实际生效**的权限与事件订阅。
- **权限错误会卡片化提示**:Feidex 会把带 scope 申请链接的权限错误转成飞书卡片(`internal/app/maintenance/runtime_service.go`),按卡片里的链接申请权限后**记得发布版本**。

常见错误码:

| 错误码 | 含义 | 处理 |
| --- | --- | --- |
| 99991672 | 缺少应用权限(报文直接给出所需 scope 与申请链接) | 按链接申请 → 发布版本 |
| 230027 | 消息/卡片接口权限不足 | 同上 |
| 1061073 | 云空间接口缺 scope | 补 `drive:*` 权限 |
| 99991400 | 群公告编辑频控 | 稍后重试,无需处理 |
| 231003 | reaction 目标消息不存在(消息已撤回/删除) | 无需处理 |
