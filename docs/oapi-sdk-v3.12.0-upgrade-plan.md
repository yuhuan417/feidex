# 飞书 Go SDK 升级到 v3.12.0 计划

> 更新时间:2026-09-30
>
> 适用范围:`github.com/larksuite/oapi-sdk-go/v3` 的版本升级,以及随之接入的 SDK 新能力(`internal/feishu`、`internal/config`)。
>
> 状态:**计划中,尚未开工**。本文档只记录结论与步骤,不含代码改动。
>
> 事实来源:本文所有版本差异均由 `v3.5.3` 与 `v3.12.0` 两个已发布模块的源码 diff 得出。SDK 的 `changelog.md` 停留在 v3.0.11,GitHub 上也没有 v3 系列的 release notes,因此不引用发布说明。

## 一、目标与范围

把 SDK 从 `v3.5.3` 升到 `v3.12.0`,并接入本次确认要用的新能力。范围按已确认的决策为**全量档**:

| 编号 | 内容 | 状态 |
| --- | --- | --- |
| P0 | token 清除逻辑前置适配(升级阻塞项,与升级同批发布) | 已定案 |
| P1 | 版本升级 + 编译期修复 | 已定案 |
| P2 | 用 SDK ws 客户端替换自研长连接层,**并接入 channel 重建事件层**(卡片回调自持) | 已定案(见 D4/D5、3.3、3.4) |
| P3 | 用 `scene.RegisterApp` 替换手写注册流程 | 已定案 |
| P4 | 接入 `outbound.SplitWithCodeFences` 到文本兜底路径 | 已定案(实现阶段找到接入点) |

## 二、现状核对(实测事实)

以下结论均为实测,不是推测:

| 项目 | 结论 |
| --- | --- |
| Go 版本要求 | v3.12.0 的 `go.mod` 只要求 `go 1.17`,本项目是 `go 1.23.0`,**不需要动 toolchain**,CI 的 `go-version-file: go.mod` 也不用改 |
| 传递依赖 | 仍是 `gogo/protobuf` + `gorilla/websocket`,**无新增** |
| 编译期破坏面 | 3 个常量、共 5 处生产代码 + 1 个测试文件 |
| 行为破坏面 | **只有 1 处**:token 刷新自愈失效 |
| 测试结果 | 改完常量后 1114 通过 / 1 失败 / 2 跳过,唯一失败即上述 token 问题 |
| `CodeError` 判定 | 新增的 `shouldSkipCodeErrorPreDecode` 只作用于 OAuth token 路径,不影响现有的 `isFeishuTenantAccessTokenInvalid` |

### 2.1 编译期破坏点

v3.7 之后 SDK 把共享枚举拆成了按 API 作用域命名的常量:

| 旧(v3.5.3) | 新(v3.12.0) | 值 | 位置 |
| --- | --- | --- | --- |
| `larkim.ReceiveIdTypeChatId` | `larkim.CreateMessageV1ReceiveIDTypeChatId` | `chat_id` | `internal/feishu/adapter.go:628`、`:744` |
| `larkim.UserIdTypeUrgentAppMessageOpenId` | `larkim.UrgentAppV1UserIDTypeOpenId` | `open_id` | `internal/feishu/adapter.go:838` |
| `larkim.UserIdTypeGetMessageOpenId` | `larkim.GetMessageContentV1UserIDTypeOpenId` | `open_id` | `internal/feishu/adapter.go:866`、`:1551` |

**三个枚举的语义(供实现时判断映射是否合理)**:

| 枚举 | 所属 API | 含义 | 取值域 | 项目取值理由 |
| --- | --- | --- | --- | --- |
| `ReceiveIdType` | `im.message.create` | **消息接收者**的 ID 类型 | open_id / union_id / user_id / email / chat_id | 取 `chat_id`:消息发往群,以群 ID 标识接收方 |
| `UserIdType` | `im.message.get` | 拉取消息时**返回的发送者**用哪种 ID | open_id / union_id / user_id | 取 `open_id`;`:866` 处另以同值比对事件回传的 `Sender.IdType` |
| `UserIdType` | `im.message.urgent_app` | 加急消息**接收者**的 ID 类型 | open_id / union_id / user_id | 取 `open_id` |

校验要点:**替换后值必须不变**——`chat_id` 不能变成 `open_id`,反之亦然。这类错误编译期不报,只会在运行时表现为发错对象或取不到消息。

**映射依据(位置对齐,非名称相似)**:v3.12.0 的枚举常量块与 v3.5.3 逐块对应,本次三处均落在同名同位的块上。核对方法为按 `const` 块顺序逐一比对,而非按名称猜测——两版块数分别为 40 与 42,v3.12.0 在块 #36 与 #40 处各插入一个新块(`UserIdType*` 与 `UserIdTypeBatchQueryMessageReaction*`,对应新增的 `SearchMessage`、`BatchQueryMessageReaction` 接口),插入点之后的块序整体后移一位,故不可按绝对序号对齐。

**关于 `GetMessageContentV1UserIDType*` 的归属**:该组即 v3.5.3 `UserIdTypeGetMessage*` 的重命名(两版均在块 #32,3 项,值与注释逐字相同)。名称中的 `Content` 不代表它属于 `im.message.resource.get`——`GetMessageResourceReqBuilder` 只有 `MessageId`/`FileKey`/`Type` 三个方法,**没有 `UserIdType` 参数**,不可能是该组的归属方。`im.message.get` 的 `GetMessageReqBuilder` 是唯一候选。

> 更正:本计划早先版本曾判定此处"无对应常量",并称 `GetMessageContentV1UserIDTypeOpenId` 属于另一个 API。两点均不成立,原因见下。

> **教训记录**:该错误源于两处失误叠加——(1) 检索命令 `grep ... | grep -v "func \|//"` 把所有常量定义行过滤掉了(它们均带行尾 `//` 注释),空结果被误读为"不存在";(2) 依名称推断归属却未加验证。更根本的是,当时以"改完能编译"作为映射正确的依据——而这三个常量的值都是 `"open_id"`/`"chat_id"` 一类字符串,**传错同样能编译通过**,编译通过只证明常量存在与类型匹配,不证明值正确。

测试文件 `internal/feishu/adapter_http_more_test.go` 也引用了前两个常量,需同步。

### 2.2 token 缓存 key 格式变更(行为破坏点)

SDK 改了 token 缓存 key 的构造方式:

```
v3.5.3  : tenant_access_token-{appID}-{tenantKey}
v3.12.0 : tenant_access_token:app_secret:{appID}:{sha256(appSecret)}:{tenantKey}
```

而 `internal/feishu/token_refresh.go:80` 写死了旧格式前缀:

```go
prefix := "tenant_access_token-" + appID + "-"
```

**注意:该前缀在 v3.5.3 上是正确的**,与当时的 key 格式一致,自愈功能当前工作正常。这是**升级才会引入**的缺陷,不是现存缺陷。

升级后该前缀匹配不到任何 key,`cleared` 恒为 0。client 虽会重建、重试仍会发起,但失效 token 还在缓存里,于是再次返回 99991663 —— **表现为 token 过期后无法自愈**。

这一点在真实环境下比单元测试里更严重:测试只是红一条,线上是长连接机器人彻底失去响应能力直到重启。

### 2.3 一个影响修法的 SDK 设计

`larkcore.NewCache(config)` 会把传入的 cache 写进**包级全局变量**:

```go
func NewCache(config *Config) {
    if config.TokenCache != nil {
        tokenManager = TokenManager{cache: config.TokenCache}
        appTicketManager = AppTicketManager{cache: config.TokenCache}
    }
}
```

两个含义:

1. "换一个 cache 实例"必须配合重建 client 才生效,单换实例无效。
2. 进程内若存在多个不同 app 的 client,后建的会覆盖先建的 token manager。本项目**恰恰是多应用**(见 3.1.1),当前之所以无碍,仅因所有 client 共用同一个 cache 实例;该前提无守护,见 3.1.4 第 2 点。

另需知道:缓存里除 tenant token 外还存 **app ticket**,其 key 是 `{prefix}-{appID}`,**仍是短横线格式**——本次只有 token 的 key 变了。

## 三、决策记录

| # | 决策 | 选定方案 | 理由 |
| --- | --- | --- | --- |
| D1 | 升级范围 | 全量档(P0–P3) | 一次性完成,避免分多次踩同一个阻塞项 |
| D2 | token 清除判据 | **按 `:` 切分取 appID 段**清除,不拼完整 key 前缀 | 不绑定 token 类型前缀与 fingerprint,且能多应用隔离;残留的分隔符耦合以金丝雀兜底,见 3.1.3/3.1.4 |
| D3 | 升级路径 | 直接升到 v3.12.0,不做逐版本 | 每个中间版本都带同样的 token cache 破坏,分步只是把同一修复做三遍 |
| D4 | channel 抽象 | **采用为事件骨架**,但卡片回调由项目自持(不调用 `ch.OnCardAction`) | 其卡片处理器无法回写回调响应,而 98% 的回调路径依赖 toast;绕开该路径即可用其消息层能力,见 3.2 与 3.4 |
| D5 | ws 长连接 | **整体替换为 SDK 客户端**,不保留任何自研逻辑 | 现有实现是拿 gorilla 裸写的 SDK 同款逻辑,替换为净收益,见 3.3;墙钟看门狗的去留另见 D7 |
| D6 | 验证策略 | **全量单测 + 关键三项真实环境验证** | 三项为 token 过期自愈、休眠/唤醒恢复、卡片回调与 channel 共存;其余四项靠单测加上线后观察,见 6.2 |
| D7 | 墙钟看门狗 | **去掉**,不实现恢复动作 | 休眠唤醒后读操作通常立即失败并走 SDK 重连路径;残留风险已知并接受,见 3.4.4 |
| D8 | 3.5.4 ① (HTTP 出站失败回收) | **接受降级**,不在 SDK 之上补回 | ②③ 本就不可恢复;补回 ① 需连带重建 D7 刚去掉的流水线重建能力,成本不划算 |
| D9 | channel 采用边界 | **尽量用,但发送路径与卡片回调除外** | channel 的 Send/Stream 只有重试、无 QPS 节流,替换会丢限流保护,见 3.4.7 |
| D10 | 发布窗口 | **独立窗口**,避开其他大改动同期 | P2 同时替换长连接与事件层,混发会使故障难以归因,见 6.2 |

### 3.1 D2:多应用前提下的隔离

#### 3.1.1 运行形态:单进程多应用

`NewService` 遍历 `cfg.ResolvedFrontends()` 为每个 frontend 建一个 App(`internal/feishuapp/service.go:33-40`),而 `FrontendConfig` 内嵌 `FeishuConfig`,即**每个 frontend 一套自己的 appID/appSecret**;adapter 经工厂逐个创建(`internal/feishuapp/deps.go:44`)。

同时 `sharedFeishuTokenCache` 是包级单例(`internal/feishu/token_refresh.go:21`),所有应用的 client 都传同一个实例(`token_refresh.go:95`)。加之 SDK 在请求时取用的是**包级全局** `tokenManager`(`core/reqtranslator.go:149`),因此**所有应用的 token 共处一个 map,靠 appID 区分**。

> 更正:本计划早先版本曾断定"一个进程服务一个飞书应用",依据是 `Adapter` 在代码中只有一处构造点。该推理把**构造点数量**误当作**运行时实例数量**——`feishu.New` 是被工厂按 frontend 逐个调用的。实际为单进程多应用。

#### 3.1.2 因此"清空整个 cache"不可用

若按早先方案全量清空,A 应用 token 过期将连带清掉 B、C 的 token,导致:

- 一次刷新放大为 N 个应用的 token 重新拉取;
- 多应用互相触发时形成**交叉清空 + 拉取风暴**;
- 飞书对 `tenant_access_token` 接口本身有限流,风暴可能阻塞全部应用。

不是正确性缺陷,但会把单应用的鉴权抖动放大到整个进程。

#### 3.1.3 选定判据:按 `:` 切分取 appID 段

v3.12.0 中**只有 token 类的 key 使用 `:`**,app ticket 仍为 `-`:

```
token(全部 ":" 分隔,core/tokenmanager.go:160-169)
  app_access_token:app_secret:{appID}:{sha256(appSecret)}
  tenant_access_token:app_secret:{appID}:{sha256(appSecret)}:{tenantKey}
  tenant_access_token:client_assertion:{appID}:{tenantKey}:{aud}

app ticket(仍 "-" 分隔,core/appticketmanager.go:50)
  {appTicketPrefix}-{appID}
```

故判据为:**将 key 按 `:` 切分,任一段等于 appID 即归属该应用并删除**。

该判据的效果:

- 清掉本应用全部 token(`tenant_access_token:*` 与 `app_access_token:*`);
- **不动 app ticket**——与原实现语义一致(原前缀 `tenant_access_token-{appID}-` 同样只清 tenant token);
- 新增的 client assertion 等 key 类型自动覆盖;
- 多应用下互不误伤。

与原实现的一处差异:原前缀只匹配 tenant token,**不匹配 `app_access_token`**(v3.5.3 中该 key 为 `app_access_token-{appID}`,与 `tenant_access_token-` 前缀不符)。新判据会一并清掉它。本项目使用 tenant token 模式,该 key 通常不存在,实践中无影响;此处记录以备核对。

**注意不得按 `_` 切分**——appID 形如 `cli_xxx` 本身含下划线,切分会破坏 appID。

**关于 app ticket**:其 key 可能存在于缓存中——`NewEventDispatcher` 会自动注册 `app_ticket` 事件处理器(`event/dispatcher/dispatcher.go:57`),飞书定期推送该事件,SDK 收到后写入缓存(TTL 12 小时)。但**本项目并不消费它**:dispatcher 以 `NewEventDispatcher("", "")` 构造(`internal/feishu/events.go:113`),加密密钥为空,事件不经解密;`internal/feishu` 内亦无任何 `AppTicket` 引用。故不清理它是更保守的选择,而非必需。

#### 3.1.4 残留风险与金丝雀

1. **本判据重新绑定了分隔符**。而分隔符恰是从 `-` 改为 `:` 才引发本次缺陷——若将来 SDK 再次变更分隔符,**现有测试不会失败**(测试断言的是我们自己假设的格式),只会像这次一样在线上表现为"token 过期后无法自愈"。

   因此增设**金丝雀**:在 `refreshClientAfterInvalidTenantToken`(`token_refresh.go:109`)中,当 `cleared == 0` 时打 `warn`。正常情况该值不应为 0(能收到 99991663 说明确实使用过 token),一旦某日开始恒为 0,日志中即可发现。

2. **一个需要守护的不变量**:`larkcore.NewCache` 会把 cache 写入包级全局(`client.go:275`)。当前能正常工作,完全依赖"所有应用传同一个 cache 实例"这一无守护前提。若将来有应用传入独立 cache,全局将静默指向它,其余应用的 token 读写随之漂移。建议在 `newFeishuLarkClient`(`token_refresh.go:93`)添加注释固化该不变量。

### 3.2 channel 用作事件骨架的边界(证据)

曾评估"抛开历史包袱、直接用 SDK 的 `channel` 作为事件骨架"。结论是**条件式**的,取决于能否接受失去 toast。

#### 3.2.1 channel 能做到的:卡片动态更新

**channel 完全支持按 messageID 做卡片 patch**:

```go
func NewCardStreamController(client *lark.Client, config types.ChannelConfig, messageID string) *CardStreamController
func (c *CardStreamController) UpdateCard(ctx context.Context, card string) error
// 内部:larkim.NewPatchMessageReqBuilder().MessageId(c.messageID) → client.Im.V1.Message.Patch
```

`NewCardStreamController` 接受任意 messageID,`UpdateCard` 走 `im.message.patch`。卡片动态更新这条路径没有问题。

#### 3.2.2 channel 做不到的:toast

**先明确 toast 的投递机制**:toast 不是一条消息,不走任何发送 API。它是**卡片回调的响应体**——飞书推送 `card.action.trigger` 回调后等待回帧,toast 装在回帧 payload 里,由 `ws/client_message.go` 的 `writeEventResponse` 写入。因此"能否 toast"等价于"能否决定回帧内容"。

`Channel` 的卡片处理器签名只能返回 `error`:

```go
OnCardAction(handler func(ctx context.Context, event *types.CardActionEvent) error)
```

底层包装闭包(`channel/channel.go:563-596`)的**每一条返回路径**第一个返回值都是 `nil`:

| 路径 | 位置 | 返回值 |
| --- | --- | --- |
| 去重命中 | `:569` | `nil, nil` |
| 未抢到锁 | `:573` | `nil, nil` |
| 内层 handler 报错 | `:585` | `err`(内层) |
| 内层 handler 正常 | `:588` | `nil`(内层) |
| 内层错误冒泡 | `:592` | `nil, err` |
| 正常路径 | `:595` | `nil, nil` |

不是某条分支遗漏,而是没有任何分支能产出 `CardActionTriggerResponse`——handler 签名不接受响应,闭包手里没有可返回的对象。

**交叉验证**:

1. **全 SDK 仅此一处 toast 机制**。搜遍所有 `.go` 文件,`Toast` 只出现于 `event/dispatcher/callback/model.go`(类型定义 + 在 `CardActionTriggerResponse` 中的字段)与 `card/model.go:1108` 的 `CustomToastBody`——后者是从未被引用的死类型。卡片 2.0(`service/cardkit/`)既无 toast 也无回调响应类型。
2. **channel 自引入起未改动**。`channel/channel.go` 在 v3.8.1 与 v3.12.0 之间逐字节一致,不存在"后续版本会补"。
3. **SDK 自身测试不涉及响应**。`channel/card_action_test.go` 只断言事件解析字段,从未断言响应内容。

**设计意图**:channel 把卡片回调定位为"事件通知",收到后用 API 改状态(`UpdateCard` → `im.message.patch`)。toast 是纯粹的应答时概念,事后无法补发,不在这套模型内。

本项目对回调响应的实际依赖分布(精确统计非测试代码中的 465 处 `CardActionTriggerResponse` 字面量):

| 字段 | 数量 | 能否迁移 |
| --- | --- | --- |
| 含 `Toast` | **456(98%)** | **不能** |
| 含 `Card`(卡片更新) | 167 | 能,改走 `im.message.patch` |
| 字面量总数 | 465 | — |

**关键差异**:`Card` 有 API 等价物(`im.message.patch`),`Toast` **没有**。`Toast` 仅存在于回调响应体中(`event/dispatcher/callback/model.go:54`),飞书未提供任何可单独弹出 toast 的 API。因此 456 处 toast 属于**不可迁移**的损失;167 处卡片更新可以迁移,代价是点击到卡片变化多一次 HTTP 往返、且不在 ack 窗口内。

#### 3.2.3 为什么不能"两者兼得"

看似显然的绕法——"channel 管消息,卡片回调自己在 dispatcher 上注册"——不成立。dispatcher 对同类型回调**禁止重复注册**:

```go
// event/dispatcher/callback_dispatch.go:11
if existed {
    panic("event: multiple handler registrations for " + "card.action.trigger")
}
```

是 `panic`,不是覆盖或叠加。因此卡片回调的所有权是**排他**的。

但存在一个可行折中:**只要不调用 `ch.OnCardAction`,channel 就不会注册卡片回调**(注册动作仅发生在 `OnCardAction` 内,`Start` 只设置生命周期回调与消息处理器)。此时可让 channel 承担消息、流式、去重、生命周期,而卡片回调由项目自己在 dispatcher 上注册,toast 全部保留。

#### 3.2.4 结论

| 前提 | 结论 |
| --- | --- |
| 能接受丢失 toast(456 处) | channel 可作为事件骨架,卡片更新改走 `im.message.patch` |
| 不能接受 | **卡片回调必须自己持有**;channel 仍可作为事件骨架承担消息层,即采用 3.2.3 的折中 |

**本项目取后者**(见 D4 与 3.4):channel 承担消息、反应、入群等事件,卡片回调由项目在 dispatcher 上自持,456 处 toast 全部保留。

**根因**:channel 把卡片回调定位为"通知",而本项目的卡片回调是需要回写响应(尤其 toast)的请求-响应语义。这是设计取舍,不是 SDK 缺陷——但对本项目而言,toast 是 98% 的回调路径都在用的能力,故取后者。

**注**:上一轮讨论中曾表述为"channel 做不了卡片动态 patch",该表述**不准确**,已在 3.2.1 更正。缺口仅在 toast。

### 3.3 为什么 ws 反而可以整包换

**关键事实:项目当前并未使用 SDK 的 ws 客户端。** `internal/feishu/ws_runtime.go` 是用 `gorilla/websocket` 裸写的完整长连接层——自己拨号(`wsDialContext`)、自己心跳(`wsPingLoop`)、自己读循环(`wsReadLoop`)、自己探活(`wsLivenessMonitor`)、自己重组分片(`combineWSMessage`)。SDK 侧只借用了类型与常量(`larkws.Frame`、`larkws.Headers`、`larkws.NewResponseByCode`、`larkws.MessageType*`、`larkws.Header*`)。

对照可见,`handleWSDataFrame`(`ws_runtime.go:180-242`)与 SDK 的 `handleDataFrame` + `writeEventResponse`(`ws/client_message.go:61-140`)是**逻辑等价的实现**:取 header → 多片重组 → 按 MessageType 分支 → 调 dispatcher → 把响应 marshal 回 frame 写回。

更重要的是,**项目已经在使用 SDK 的 dispatcher 注册处理器**(`internal/feishu/events.go:69` 的 `d.OnP2CardActionTrigger`)。因此替换 ws 客户端只动 transport 层,**dispatcher 以上(含全部卡片回调逻辑)一行不用改**——3.2 里 channel 卡住的那件事在 ws 层不存在。

SDK 在 v3.11.0 补齐的能力与现有自研实现对应关系:

| 自研实现 | SDK 对应 |
| --- | --- |
| `wsPingLoop` | 内建 ping,间隔由 `ClientConfig.PingInterval` 控制 |
| `wsReadLoop` | `receiveMessageLoop` |
| `combineWSMessage` | `Client.combine` |
| `shouldReconnectTransport` + `runWSLoop` 重连延迟 | `WithAutoReconnect` + `reconnectAfterFailure` + `ClientConfig.ReconnectCount/ReconnectInterval/ReconnectNonce` |
| `closeWSConn` | `Close()` / `CloseAndWait(ctx)` |
| `currentWSWriteTimeout` | `WithWriteTimeout` |
| 静默断连检测 | `setReadDeadline` + `pongWait = 2*pingInterval + pongGracePeriod`(`ws/client_session.go:30`) |

**必须保留 `wsWallClockAction`(墙钟跳变检测)。** 它处理的是笔记本合盖休眠/唤醒场景:SDK 的读超时基于单调时钟,休眠期间不推进,唤醒后可能长时间不触发,连接看似存活实则已断。feidex-pc 是桌面端,此场景真实存在,SDK 未覆盖。

**但保留方式与早先设想不同**——原计划为"建立在 `SetOnReady`/`SetOnDisconnected` 之上触发重连",该方案不成立,原因见 3.4.4。

### 3.4 channel 集成方案与硬约束

决策采用 channel 作为事件骨架、卡片回调由项目自持(D4)。以下约束均为实测所得,其中三条直接决定实现形态。

#### 3.4.1 channel 与 SDK ws 客户端绑定,不可分开

`channel.NewChannel(client *lark.Client, wsClient *larkws.Client, opts ...)` 要求传入 `*larkws.Client`,且 `Start` 内部即 `ch.wsClient.Start(ctx)`。因此**采用 channel 接收事件必须以替换 ws 客户端为前提**——P2 与 P4 由此合并为同一工作项,不能分先后。

(注:`wsClient` 可为 nil,此时 `Start` 直接返回,channel 退化为 send-only,但这正好失去接收事件的能力,不满足需求。)

#### 3.4.2 卡片回调自持:不得调用 `ch.OnCardAction`

channel 的五处 dispatcher 注册中,`card.action.trigger` 与项目自持的卡片回调冲突。dispatcher 对同一回调类型**禁止重复注册且直接 panic**(`event/dispatcher/callback_dispatch.go:11`),因此:

- **不调用 `ch.OnCardAction`**,channel 便不会注册卡片回调(注册动作仅发生在该方法内,`Start` 不涉及);
- 项目继续通过 `internal/feishu/events.go` 注册 `OnP2CardActionTrigger`,toast 与 in-ack 卡片更新能力完整保留。

#### 3.4.3 生命周期钩子必须走 channel,不能走 `wsClient.SetOn*`

`ws` 客户端的 `SetOnReady` / `SetOnError` / `SetOnReconnecting` / `SetOnReconnected` / `SetOnDisconnected` 均为**覆盖**语义(`c.onReady = f`,`ws/client.go:165-193`)。而 `channel.Start` 会**无条件调用全部五个** `SetOn*` 并写入自己的转发闭包(`channel/channel.go:294-327`)。

因此任何在 `ch.Start` 之前设置的回调都会被静默覆盖。项目若需监听生命周期,必须使用 channel 自己的 `OnReady` / `OnError` / `OnReconnecting` / `OnReconnected` / `OnDisconnected`——这些是**追加**语义(`ch.onReadyHandlers = append(...)`)。

#### 3.4.4 墙钟看门狗:去掉(D7),附带记录一项 SDK 约束

**决策**:去掉墙钟看门狗(`wsWallClockAction` 及 `wsLivenessMonitor` 中的墙钟分支),不实现任何恢复动作。理由与残留风险:

- 休眠唤醒后 TCP 连接通常已被对端或本机网络栈断开,读操作会立即失败并进入 SDK 的正常重连路径(`runCoordinator` 的 `for conn != nil` 循环),此时看门狗是冗余的。
- **残留风险**:若存在"连接看着活着、实则已死、读也不报错"的场景,则最坏情况下要等 SDK 读超时(默认 ping 2 分钟 → `pongWait` 245 秒)才恢复。该风险已知并接受。
- 该风险无法靠单元测试覆盖,列入 6.1 实测项。

**附带记录(本次不需要,但会绊住后来者)**:去掉看门狗后,P2 不再需要"整条流水线重建"的能力。但以下三条约束是真实存在的,若将来有人想加"重启连接"一类的逻辑会立即撞上:

1. `ws.Client` **一次性**:`stopRun` 在 `runStopByClose`(调用 `Close()`)或 `runStopByContext`(ctx 取消)时置 `c.terminal = true`(`ws/client_lifecycle.go:125-127`),此后 `beginRun` 返回 `errClientTerminal`,错误文本为 **"websocket client cannot be restarted"**。`Close()` 之后该实例永久不可复用。
2. `channel` 与 `wsClient` 一一绑定,ws 实例作废即 channel 作废。
3. dispatcher **禁止重复注册**:`OnP2MessageReceiveV1` 与 `OnP2CardActionTrigger` 同样 panic(`event/dispatcher/im_v1_event_dispatch.go:167-170`),旧 dispatcher 无法复用于新 channel。

即:真要重建,只能是"新建 dispatcher → 新建 `larkws.Client` → 新建 channel → 重新注册全部 handler"这一整条。届时构造逻辑需收敛为可重复调用的函数,不能散落在包级初始化里。

#### 3.4.5 覆盖范围差异

channel 注册 5 个事件:`OnP2MessageReceiveV1`、`OnP2MessageReactionCreatedV1`、`OnP2MessageReactionDeletedV1`、`OnP2ChatMemberBotAddedV1`、`OnP2CardActionTrigger`(自持后不注册)。

项目当前注册 5 个:`OnP2MessageReceiveV1`、`OnP2MessageRecalledV1`、`OnP2MessageReactionCreatedV1`、`OnP2CardActionTrigger`、`OnP2ChatMemberBotAddedV1`。

差异:

- **`OnP2MessageRecalledV1` channel 不覆盖**(channel 全包内无 recall 相关代码)。项目继续自行注册,不冲突。
- channel 额外覆盖 `OnP2MessageReactionDeletedV1`,项目当前未使用,可选择性接入。

#### 3.4.6 消息模型存在缺口,不能直接替换

channel 的 `NormalizedMessage`(`channel/types/types.go:55`)与项目的 `InboundMessage`(`internal/feishu/adapter.go:31`)字段并不对齐,缺口如下:

| 项目 `InboundMessage` 需要 | `NormalizedMessage` | 说明 |
| --- | --- | --- |
| `RootMessageID` / `ParentMessageID` / `ThreadID` | **无** | 项目的会话线程模型依赖这三个字段 |
| `MergeForwardMessageIDs` / `ExpandedMergeForward` | **无** | 合并转发展开,项目自有的处理 |
| `UserName` / `ChatName` | **无** | 需另行解析 |
| `SessionKey` / `MentionedOpenIDs` / `MentionedSelf` | 部分 | `Mentions` 可推导,`SessionKey` 需自建 |
| — | `RawEvent` | channel 保留了原始 `P2MessageReceiveV1` |

**因此 channel 替换的是"事件接入与过滤管道",不是消息数据模型。** 项目仍需从 `NormalizedMessage.RawEvent` 取出原始事件,构建自己的 `InboundMessage`。

#### 3.4.7 采用边界(D9:尽量用,两处除外)

原则:**凡 channel 不弱于现有实现的,一律采用;凡 channel 更弱的,保留项目实现。**

| channel 能力 | 是否采用 | 依据 |
| --- | --- | --- |
| 事件接入:`OnMessage` / `OnReaction` / `OnBotAdded` | **采用** | 含归一化、自回复抑制、被 @ 判定、按会话串行、事件级去重(`IsDuplicate` + `processLock` + `pipelineManager`),不弱于现有实现 |
| `OnReject` + `PolicyConfig` / `SafetyConfig` | **采用**(新增能力) | 项目无对应物 |
| 生命周期回调 `OnReady` / `OnError` / `OnReconnecting` / `OnReconnected` / `OnDisconnected` | **采用** | 见 3.4.3,且是唯一可用途径 |
| `GetBotIdentity` | **采用** | 项目无对应物(现有 bot 信息获取另在 `internal/feishuapp/bot_profile.go`) |
| **`DownloadFile`** | **不采用** | 二者调用的 API 不同:channel 用 `Im.V1.Image.Get(image_key)` / `Im.V1.File.Get(file_key)`,**不接受 message_id**;项目用 `Im.V1.MessageResource.Get`,必须带 `MessageId + FileKey + Type`(`adapter.go:987`),这是从收到的消息里取附件的正确接口。此外 channel 只返回裸 `[]byte`,不提供项目所需的文件名解析(`resolveDownloadedFileName`),也不参与 token 自愈重试 |
| **`Send` / `Stream` 发送路径** | **不采用** | channel 只有重试(`outbound.Retry`),**无 QPS 节流**;项目有 `create` / `patch` / `announcement` 三档 QPS 限流(`feishuMessageCreateQPS = 5` 等)与按 messageID 分桶的 patch pacer(`ensurePatchPacer`)。替换会丢限流保护 |
| **`OnCardAction` 卡片回调** | **不采用** | D4 与 3.4.2,toast 无法经其回写 |
| **`inbounddedup.Deduper`** | **保留**(与 channel 去重共存,非二选一) | 二者解决不同问题:channel 的 `IsDuplicate` 是单阶段 LRU,`processLock` 只在 handler 同步执行期持有;项目的 `Claim`/`MarkDone` 是两阶段状态机(in-flight 2 分钟 / recently-done 10 分钟),覆盖**异步处理的整个窗口**——这正是 Feishu 事件在长任务期间被重投的场景 |

#### 3.4.8 多应用下的安全性

本项目为单进程多应用(3.1.1),每个 frontend 各有一个 Adapter,因此替换后将出现**多个 `larkws.Client` + 多个 channel + 多个 dispatcher 并存**。经核实:

- **`ws` 与 `channel` 包内无包级可变状态**(仅有 protobuf 生成代码的占位 `var` 与错误定义),各自的客户端实例互不干扰——这一点与 `larkcore.NewCache` 写包级全局的设计**不同**(见 3.1.4 第 2 点),不存在后者那类互相覆盖的风险。
- `dispatcher.NewEventDispatcher` 只写实例字段(`eventType2EventHandler` 等),不涉及全局。

**已知的副作用(日志噪音,非缺陷——接受)**:`channel.Start` 在 `SetOnReady` 内通过事件日志打印一段多行中文横幅("使用长连接接收事件/回调…"及 bot 身份信息)。多应用下每个 channel 各打印一次,且只在连接成功时打印。该横幅包含 bot 身份,有排查价值,故**接受,不做特殊处理**。

### 3.5 全盘替换 ws 的影响面

范围:删除 `internal/feishu/ws_runtime.go` 全部 597 行,以及 `adapter.go` 中约 15 个 ws 相关字段与常量。

#### 3.5.0 完整删除清单

删除范围**不止 `ws_runtime.go` 一个文件**——以下散落在 `adapter.go` 中的部分同样要处理:

| 位置 | 内容 | 处理 |
| --- | --- | --- |
| `ws_runtime.go` | 全文 597 行 | 删除 |
| `adapter.go:144-150` 附近 | `wsMu` / `wsWriteMu` / `wsConn` / `wsServiceID` / `wsFragments` / `wsPingInterval` / `wsReconnectInterval` / `wsLastRxAt` / `wsLastPingAt` 等字段 | 删除(`wsServiceID` 经核实仅在本文件内使用) |
| `adapter.go:171-180` | `wsDefaultPingInterval` / `wsDefaultReconnectInterval` / `wsMaxReconnectInterval` / `wsMinReadTimeout` / `wsMonitorTick` / `wsPongGrace` / `wsProbeTimeout` / `wsRecycleCooldown` / `wsFragmentCacheTTL` | **全部删除**。前两者由 SDK 自带默认值接管(见 P2 第 7 条),其余属被删逻辑 |
| `adapter.go:314` | `a.wsFragments = larkcache.New(30*time.Second)` | 删除;分片重组由 SDK `Client.combine` 接管 |
| `adapter.go:24` | `larkcache` import | 删除(经核实全包**仅此一处**使用) |
| `adapter.go:334` | `validateWSStartup` | 见 P2 第 11 条 |
| `adapter.go:352` | `fetchWSEndpointURL` | 见 P2 第 11 条 |
| `adapter.go:394` | `Stop()` | 见 P2 第 12 条 |

#### 3.5.1 不受影响:配置面

`config.example.toml` 无任何 ws 调参项;`config.go` 中的 `WSURL` / `WSBearerToken` 属 Codex 后端,与飞书无关。**全盘替换不改变用户配置面。**

服务端下发的参数(ping 间隔、重连次数/间隔/nonce)由 SDK `applyConfigLocked`(`ws/client.go:248-253`)完整接管,与项目 `applyWSClientConfig` 等价。

#### 3.5.2 SDK 完全覆盖(净删除)

| 项目实现 | SDK 对应 |
| --- | --- |
| `runWSLoop` 重连循环 | `runCoordinator` 的 `for conn != nil` 循环 |
| `wsReconnectDelayForExit` | `ClientConfig.ReconnectInterval` + `reconnectAfterFailure` |
| `wsPingLoop` / `wsReadLoop` | 内建 ping / `receiveMessageLoop` |
| `combineWSMessage` | `Client.combine` |
| `handleWSDataFrame` / `handleWSControlFrame` | `handleDataFrame` / `handleControlFrame` + `writeEventResponse` |
| `wsDialContext` | 内建拨号 + `WithWebSocketDialer` |

#### 3.5.3 量级差异(可接受,但属行为变更)

| 项 | 项目 | SDK | 说明 |
| --- | --- | --- | --- |
| 静默断连宽限 | `ping*2 + 30s`(默认 270s) | `ping*2 + 5s`(默认 245s) | `pongGracePeriod = 5s` 写死(`ws/client_session.go:28`),无配置项。SDK 判定快 25 秒 |
| 写超时 | 15s(`wsDefaultWriteTimeout`) | 默认 10s | **已决定显式设置 `WithWriteTimeout(15*time.Second)` 保持原值**(P2 第 7 条);不设置则会静默改变 |

#### 3.5.4 能力损失

项目对"连接已坏"有**三条**独立的发现路径,SDK 只保留了其中最弱的一条(被动读超时)。三者机制不同,可恢复性也不同——**只有 ① 可恢复**。

**① HTTP 出站失败不再触发 WS 回收 —— 可恢复,但需要重建能力**

项目在 11 处 HTTP 调用点(`adapter.go` 9 处、`announcement.go` 3 处、`message_rate_limit.go` 2 处,含 `im.message.create` / `patch` / `reaction` 等)调用 `noteOutboundTransportFailure(err)`。其启发式是:**HTTP 发不出去,说明网络多半已断,于是主动关掉 WS 连接提前触发重连**,带 5s 冷却防抖。

SDK 无等价机制。但这条路径的触发点在**项目自己控制的 HTTP 调用点**上,因此**可以在 SDK 之上恢复**——代价是恢复动作(关连接)只能走 `ws.Client.Close()`,而它是终态的,所以必须连同"整条流水线重建"一起建回来(3.4.4)。

**② WS ack 写失败不再触发重连 —— 不可恢复**

项目 `handleWSDataFrame` 末尾写入回帧失败时返回错误,经 `wsReadLoop` → `failWSLoop` 进入重连:

```go
if err := a.writeWSMessage(gws.BinaryMessage, out); err != nil {
    return fmt.Errorf("write websocket reply frame: %w", err)
}
```

SDK 侧同一位置只记日志,不做任何后续动作(`ws/client_message.go`):

```go
if err := c.writeMessage(run, ws.BinaryMessage, message); err != nil {
    c.logger.Error(...)   // 仅日志
}
```

该失败在 SDK 内部,不向调用方暴露回调或错误,故**无法在 SDK 之上恢复**。

**③ 主动探针机制消失 —— 不可恢复**

项目 `wsLivenessMonitor`(`:495`)在 ping 后 15s 内无入站流量即主动探测,10s 超时即重连(`wsShouldStartProbe` / `wsProbeTimedOut`)。

SDK 的 ws 客户端**未导出任何可用于观测连接活动的接口**——导出方法仅有 `SetOn*`、`EventHandler`、`Start`、`Close`、`CloseAndWait`,无法注入 ping 帧,也无"最后活动时间"读取接口。channel 同样不暴露底层连接。故**无法在 SDK 之上恢复**。

**综合后果**:连接"半死"(写已不通、读尚未报错)时,项目可在秒级发现并重连,SDK 只能等被动读超时——默认配置下最长 245 秒(3.5.3)。该窗口内事件收不到、回复发不出。

**决策(D8)**:① **接受降级,不补回**。②③ 本就不可恢复;补回 ① 需连带重建 D7 刚去掉的流水线重建能力,成本不划算。上述 245 秒窗口作为已知风险接受,列入 6.1 的"休眠/唤醒恢复"实测项一并观察。

#### 3.5.5 新增风险:服务端配置被无保护覆盖

**项目当前的写法是防空的**——服务端下发的配置逐项判零后才采用(`ws_runtime.go:250-255`):

```go
if conf.PingInterval > 0 { a.wsPingInterval = ... }
if conf.ReconnectInterval > 0 { a.wsReconnectInterval = ... }
```

**SDK 不判零,无条件覆盖**(`ws/client.go:248-253`):

```go
func (c *Client) applyConfigLocked(conf *ClientConfig) {
	c.reconnectCount = conf.ReconnectCount
	c.reconnectInterval = time.Duration(conf.ReconnectInterval) * time.Second
	c.reconnectNonce = conf.ReconnectNonce
	c.pingInterval = time.Duration(conf.PingInterval) * time.Second
}
```

**风险**:若服务端的 endpoint 响应中任一字段缺省(返回 0),SDK 会用 0 覆盖构造期的合理默认值:

- `pingInterval = 0` → `pongWait(0) = 0 + 5s = 5 秒`读超时 → 连接在最后一条入站帧后 5 秒即被判死 → **持续重连抖动**;
- `reconnectInterval = 0` → 立即重连,形成热循环。

原实现因为有判零保护,不会出现这种情况。

**缓解措施(金丝雀)**:本项无法在不改 SDK 的前提下拦截,但**保留的预检恰好能看到同一份配置**——`EndpointResp.Data.ClientConfig`(`ws/model.go:13`)与 SDK 内部使用的是同一个字段。故在预检中**记录服务端下发的 `PingInterval` / `ReconnectInterval` / `ReconnectCount` / `ReconnectNonce`**:若出现 0,启动日志即可发现,不必等到重连抖动发生后才排查。

#### 3.5.6 测试影响

9 个用例直接覆盖 ws 内部,**8 个随函数删除**(`TestWSWallClockAction` 按 D7 一并删除):

```
TestShouldReconnectTransport              TestWSShouldStartProbe
TestWSReconnectDelayForExit               TestWSProbeTimedOut
TestCurrentWSReadTimeoutUsesSafetyWindow  TestCloseWSConnWhileWriteBlockedDoesNotWaitForStateLock
TestWSWallClockAction                     ← D7 决定删除
```

2 个需重写:`TestAdapterStartSuccessAndWSValidationBranches`、`TestFetchWSEndpointURLAndValidateWSStartupErrors`。

## 四、工作分解

### P0 — token 清除逻辑的前置适配(**与升级同批发布**)

**性质澄清**:这不是修复线上缺陷,而是**预适配**。v3.5.3 上 `clearTenantAccessTokens` 匹配的 `tenant_access_token-{appID}-` 与当时的 key 格式**一致,工作正常**(`TestAdapterRefreshesTenantTokenForReplyAndReaction` 在当前代码上通过,已实测)。本次改动是趁其仍能工作时,把判据改为不编码 key 格式,以免升级后静默失效。

**为何不与升级分离发布**:该改动在 v3.5.3 上行为完全等价——两种判据命中同一批 key,发出去既观察不到效果也无法验证,徒增一轮流程。故与升级同批。

**无需兼容旧 key 格式**:`resettableLarkTokenCache` 为纯内存 map(`token_refresh.go:23-26`),全仓仅三处引用(声明、`WithTokenCache` 接线、清除调用),不落盘、不序列化。新进程启动即空缓存,**不存在跨版本迁移**。仅当本改动单独发布在 v3.5.3 上时,旧格式才会成为运行时格式——既然同批发布,该场景不存在。

**改动**:`internal/feishu/token_refresh.go`

- `clearTenantAccessTokens(appID)`(`:72-91`)改为按 3.1.3 的判据清除:将 key 按 `:` 切分,任一段等于 appID 即删除。不再匹配完整前缀 `tenant_access_token-{appID}-`。
- 保持 appID 隔离——**不可退化为全量清空**(见 3.1.2)。
- 调用点 `:114` 及日志字段(`cleared_tokens`)同步。
- 测试引用点同步:`internal/feishu/adapter_http_more_test.go:196`、`:284`,`internal/feishu/announcement_test.go:125`。
- 按 3.1.4 第 1 点,在 `refreshClientAfterInvalidTenantToken`(`:109`)增加 `cleared == 0` 的 warn 金丝雀。
- 按 3.1.4 第 2 点,在 `newFeishuLarkClient`(`:93`)补注释,固化"所有应用共用同一 cache 实例"这一前提。
- 判据处补注释,写明**三点依据**:为何按 `:` 切、为何不得按 `_` 切、为何不能退回前缀匹配。这是防止后续被"优化"回去的关键说明。

**验收**:

- 现有 token 刷新相关测试全绿。
- 新增多应用隔离用例:**构造两个不同 appID 的缓存条目,清除其一,断言另一条仍在**。这是本次改动的核心回归点。
- 新增 app ticket 不受影响用例:构造 `{appTicketPrefix}-{appID}` 条目,清除后断言其仍在(守护 3.1.3 的语义)。
- 不写旧格式(`-` 分隔 token key)兼容用例——测的是同批发布后不会出现的运行时状态。

### P1 — 版本升级与编译期修复

**改动**:`go.mod`、`internal/feishu/adapter.go`、`internal/feishu/adapter_http_more_test.go`

- `go.mod` 中 `github.com/larksuite/oapi-sdk-go/v3` 升至 `v3.12.0`,`go.sum` 随之更新。
- 按 2.1 表格修 3 处常量(映射关系已核验,直接替换即可)。
- 顺带:`go mod tidy` 会把 `gorilla/websocket` 从 `// indirect` 块挪到直接依赖块——它本来就被生产代码直接引用(`ws_runtime.go:16`、`adapter.go:22`),当前 `go.mod` 的标注与实际不符。这是既有问题,借本次一并修正。

**验收**:`go build ./...` 与 `go vet ./...` 干净;**全量测试全绿**(含 P0 已修复的 token 刷新用例——该用例在未做 P0 的 v3.12.0 上是失败的)。

### P2 — 用 SDK ws 客户端 + channel 重建事件层

**背景**:v3.8.1 引入 ws 生命周期钩子;v3.11.0 重构 ws(`client_lifecycle.go` / `client_message.go` / `client_session.go` / `client_transport.go`),补齐静默断连检测、写超时上界、生命周期可取消、两处 data race 修复与 `CloseAndWait(ctx)`。channel 亦于 v3.8.1 引入,此后 API 冻结。

**范围**:ws 层**整体替换**;同时接入 channel 作为事件骨架,**卡片回调由项目自持**(D4、3.4.2)。P2 与 P4 合并——channel 依赖 `*larkws.Client`,二者不可分开(3.4.1)。

**改动**:

1. 建立 `larkws.NewClient(appID, appSecret, opts...)`,构造 dispatcher 并通过 `WithEventHandler` 注入。注意 `Start(ctx)` 是**阻塞**的(内部 `runCoordinator` 跑到连接结束才返回),须置于 goroutine。
2. 构造 `channel.NewChannel(larkClient, wsClient)`,**只注册消息层处理器**(`OnMessage` / `OnReaction` / `OnBotAdded` 等),**不调用 `OnCardAction`**(3.4.2)。
3. **撤掉 `internal/feishu/events.go` 中与 channel 重叠的三处注册**(见下方"注册冲突")。
4. 卡片回调继续由 `internal/feishu/events.go` 注册 `OnP2CardActionTrigger`,toast 与 in-ack 卡片更新路径不变。
5. `OnP2MessageRecalledV1` 由项目继续注册(channel 不覆盖,3.4.5)。

   > **注册冲突(必做,否则 panic)**:dispatcher 对同一事件禁止重复注册且直接 panic。channel 会注册 `OnP2MessageReceiveV1`(经 `OnMessage`)、`OnP2MessageReactionCreatedV1` / `DeletedV1`(经 `OnReaction`)、`OnP2ChatMemberBotAddedV1`(经 `OnBotAdded`)、`OnP2CardActionTrigger`(经 `OnCardAction`,本项目不调用)。
   >
   > 项目 `events.go` 当前注册 5 个,其中 **3 个与 channel 重叠,必须移除**:
   >
   > | 项目当前注册 | 处理 |
   > | --- | --- |
   > | `d.OnP2MessageReceiveV1`(`:29`) | **移除**,改由 `ch.OnMessage` 接管 |
   > | `d.OnP2MessageReactionCreatedV1`(`:55`) | **移除**,改由 `ch.OnReaction` 接管 |
   > | `d.OnP2ChatMemberBotAddedV1`(`:77`) | **移除**,改由 `ch.OnBotAdded` 接管 |
   > | `d.OnP2CardActionTrigger`(`:69`) | **保留**(D4) |
   > | `d.OnP2MessageRecalledV1`(`:42`) | **保留**(channel 不覆盖) |
   >
   > 移除顺序同样重要:channel 是在 `OnMessage` / `OnReaction` / `OnBotAdded` 被调用时懒注册,若项目的注册先执行,channel 注册时即 panic。
5. 生命周期监听一律走 channel 的 `OnReady` / `OnError` / `OnReconnecting` / `OnReconnected` / `OnDisconnected`,**禁止使用 `wsClient.SetOn*`**(会被 `channel.Start` 覆盖,3.4.3)。
6. 删除自研 transport 层:`wsDialContext`、`wsPingLoop`、`wsReadLoop`、`runWSConnection`、`runWSLoop`、`combineWSMessage`、`handleWSDataFrame`、`handleWSControlFrame`、`writeWSMessage`、`writeWSPing`、`storeWSConn` / `closeWSConn` 等。
7. 现有可调参数映射(已全部核实):

   | 项目参数 | 处理 |
   | --- | --- |
   | `currentWSPingInterval` | 由 SDK 接管:构造默认 2 分钟(与项目一致),之后由服务端下发覆盖 |
   | `currentWSReconnectInterval` | 由 SDK 接管:构造默认 2 分钟(**与项目的 5 秒不同**),之后由服务端下发覆盖 |
   | `currentWSWriteTimeout`(15s) | **显式设置 `WithWriteTimeout(15*time.Second)`** 保持原值——SDK 默认为 10s,不显式设置即静默改变 |
   | `currentWSReadTimeout` | **删除**:与 SDK 的 `pongWait` 语义重叠(见 3.5.3) |
   | `currentWSServiceID` | **删除**:经核实仅用于 `writeWSPing` 构造 ping 帧,而 ping 由 SDK 内建,无其他用途 |
   | `wsFragmentCacheTTL`(5s) | **删除**:分片重组由 SDK 接管,其内部缓存为 30 秒 |
8. 评估并删除其余自研探活逻辑:`armWSProbe` / `currentWSLivenessSnapshot` / `wsProbeTimedOut` / `wsShouldStartProbe` / `currentWSReadTimeout`(与 SDK 的 `setReadDeadline` + `pongWait` 语义重叠,见 3.5.3)。
9. **墙钟看门狗一并删除**(D7):`wsWallClockAction` 及 `wsLivenessMonitor` 中的墙钟分支。删除后不再需要"整条流水线重建"能力,**无需**按可重建结构组织构造逻辑(3.4.4)。
10. **消息模型适配**:从 `NormalizedMessage.RawEvent` 取出原始 `P2MessageReceiveV1`,构建项目自己的 `InboundMessage`(线程字段、合并转发、名称解析等 channel 不提供,见 3.4.6)。
11. **处理 `Start` 的错误语义变化**(见下)。
12. **`Adapter.Stop()` 增加 ctx**:现签名 `Stop()`(`adapter.go:394`),channel 的 `ch.Stop(ctx)` 需要 context,签名需调整。

**`Start` 的错误语义变化(需决策处理方式)**

现状:`Adapter.Start(ctx)` 在 `adapter.go:320` 调用 `validateWSStartup`,做一次**完整预检**——拉取 endpoint + 实际拨号 + 立即关闭,10 秒超时。失败则同步返回 error,`startFrontend`(`internal/feishuapp/frontend_helpers.go:18`)会检查它,因此**启动期即可得知飞书连不上**。

SDK 的 `ch.Start(ctx)` 内部自连且**阻塞到连接结束**,不提供"只校验不连接"的接口。若直接替换,`Start` 会立即返回 nil,**启动失败从同步错误变成只能靠 `OnError` 异步感知**——daemon 会认为启动成功,而实际连不上。

**决策:采用"保留 endpoint 预检、去掉拨号"**。

- 保留 `fetchWSEndpointURL`(纯 HTTP POST)作为预检:它能验证**凭据正确性**与**应用是否开通长连接模式**——这正是启动期该发现、且启动期能发现的问题。`Start` 仍同步返回 error,`service.go:74-77` 的"启动失败则停止全部 app 并返回错误"语义不变。
- 去掉其中的拨号部分(`validateWSStartup` 中 `wsDialContext` 及其后的关闭):拨号失败属网络瞬时问题,SDK 会自动重连,不值得让整个 daemon 启动失败。
- 不采用"完全去掉预检":那会让 daemon 在连不上飞书时**报告启动成功**——`service.go` 的失败路径不会触发,运维只能靠机器人不回消息来发现问题。

**边界**:channel 采用范围由 D9 与 3.4.7 界定;本项无待决事项。

**验收**:

- `go build ./...` / `go vet ./...` 干净,无残留死代码。
- 断线重连相关测试全绿;删除探活逻辑的,对应测试重写为覆盖 SDK 行为的版本,而非直接删除。
- 新增用例:构造含线程与合并转发的原始事件,断言 `InboundMessage` 各字段解析正确。
- 新增用例:channel 与自持卡片回调共存——断言未调用 `OnCardAction` 时 `card.action.trigger` 未被注册,且项目回调正常触发(守护 3.4.2)。
- 真实环境断连演练见 6.1。

### P3 — 用 `scene.RegisterApp` 替换手写注册流程

**背景**:`RegisterApp(ctx, opts)` 是现有手写流程的行为等价实现——`begin` 请求参数逐字一致(`archetype=PersonalAgent`、`auth_method=client_secret`、`request_user_info=open_id`,同一个 `accounts.feishu.cn`),并额外提供:

- `OnQRCode(*QRCodeInfo)` / `OnStatusChange(*StatusChangeInfo)` 回调
- `AppPreset`(应用名、描述、头像预填)与 `Addons{Scopes, Events, Callbacks}`(建应用时声明权限与事件订阅)
- Lark 域名自动切换(检测到 `TenantBrand == "lark"` 时切到 `accounts.larksuite.com`)

最后一条尤其有价值:`docs/configuration.md:113` 目前明确写着"二维码自助注册流程只对 `feishu` 域名可用,Lark 国际版请手工创建",SDK 把这个限制解掉了。

**改动**:`internal/config/feishu_setup.go`

- `runRegistrationFlow`(`:237-311`)与 `registrationCall`(`:313-344`)由 SDK 调用取代。
- `printQRCode`(`:354`)与 `saveQRCode`(`:366`)**保留**——见下方更正。
- `SetupFeishu`(`:71`)中 `FeishuSetupMode` 的 auto/new/bind 三分支、`saveToFrontend`(`:152`)、`validateFeishuCredentials`(`:208`)均**保留**,这些是 SDK 不提供的项目自身逻辑。

**更正(重要)**:上一轮讨论中曾认为此步可顺带删除 `mdp/qrterminal` 与 `rsc.io/qr` 两个依赖。**该判断不成立。** SDK 只返回 URL 字符串(`QRCodeInfo{URL, ExpireIn}`),不渲染二维码;`scene/` 与 `core/` 内没有任何 QR 编码或 PNG 生成代码。终端打印和存图仍需项目自己实现,**两个依赖都保留**。

**三处差异的处理(已定)**:

1. **`init` action**:SDK 跳过现有流程开头的 `init`(`:241`),直接从 `begin` 开始。`init` 无参数、响应只用于检查 `Error` 字段,不携带后续流程所需的状态,属握手/预热性质。**以 SDK 为准去掉**;若真实环境注册失败,回退手段是在调用 `RegisterApp` 之前手工补一次 `registrationCall(client, "init", ...)`——该函数可暂时保留以备回退。
2. **stdout 输出**:SDK 在轮询循环内有 `fmt.Printf("tenant brand: %s\n", ...)`(仅在 `UserInfo != nil` 时),会直写 stdout。该输出发生在**二维码渲染之后**,不破坏二维码本身,最坏是几行噪音。**先接受**;若实测发现与提示信息交错造成误读,再用临时替换 `os.Stdout` 的方式包住 `RegisterApp` 调用(CLI 单线程流程,可行)。
3. **超时语义**:现有实现由调用方传 `opts.Timeout`(默认 10 分钟,`:72-73`);SDK 用 ctx + 服务端 `ExpireIn`。处理方式:**用 `context.WithTimeout(ctx, opts.Timeout)` 包住**,保留调用方可控的超时;并在错误分支把 `errors.Is(err, context.DeadlineExceeded)` 归一到原有的"超时"提示语,使 `context.DeadlineExceeded` 与 SDK 的 `ExpiredError` 对外表现一致。

**验收**:`feishu new` 全流程在真实环境跑通(第六节);`feishu bind` 不受影响。

### P4 — 接入 `outbound.SplitWithCodeFences`(已完成)

原 P4 为"按需取用 channel 内的独立工具",一度因"接入点不明确"降级。实现阶段找到了明确接入点,遂落地。

**接入点:文本兜底路径。** 卡片发送失败时会退回纯文本发送,而该路径把**完整正文**整段塞进一条消息(`internal/feishuapp/delivery.go:80`、`internal/feishuapp/reply_chunk_delivery.go:84`),`SendText` / `ReplyTextWithID` 内也没有长度处理。后果是:长回复 + 卡片发送失败 = 兜底消息自身也超限失败,**用户什么都收不到,且无任何报错**。

**实现**:

- `replyTextChunked`(`internal/feishuapp/delivery.go`)按 `outbound.SplitWithCodeFences` 切分后逐条发送。选它而非项目自有的切分,是因为它**在代码块边界断开、并在下一段重开同样的语言标记**,正合 agent 输出满是代码块的形态;项目自有的切分是按卡片 payload 字节预算(`ReplyCardMaxPayloadBytes = 20000`),两者轴不同,不冲突。
- 新增 `ReplyTextMaxBytes = 20000`(`internal/adapter/feishu/delivery/reply_card_split.go`)。**注意这是投递策略而非实测的 API 上限**——`im.message.create` 文本内容的确切限制未经验证,取值与卡片预算同量级以便统一推理。
- 返回首个已送达消息 id:首段失败则传播错误;后续段失败时,已送达的部分按成功上报并打 `warn`,以免调用方丢掉已发消息的链接记录。

| 工具 | 结论 |
| --- | --- |
| `outbound.SplitWithCodeFences` | **已接入**(见上) |
| `safety.DedupCache(capacity, ttl)` | **不采用**。能力弱于项目现有实现(只有 `IsDuplicate(key) bool`;项目的 `internal/feishuapp/inbounddedup/deduper.go` 是 `Claim`/`MarkDone` 状态机,`:38`、`:60`)。 |

**与 D9 的关系**:D9 排除的是 channel 的**发送路径**(`Send` / `Stream`,因其无 QPS 节流),`SplitWithCodeFences` 是同一个包里的纯函数,不在排除范围内。实际发送仍走项目自己的 `ReplyTextWithID`,限流保护不变。

## 五、明确不做的事

- **不做逐版本升级**(见 D3)。
- **不让 channel 接管卡片回调**(见 D4、3.4.2)。不是"存量代码舍不得扔":channel 有能力做卡片 patch(3.2.1),缺口仅在 toast 无法经其回写(3.2.2)。若将来 SDK 允许 `OnCardAction` 处理器返回响应,此结论应重新评估——届时应把卡片回调也交给 channel,可省掉 3.4.4 的整条重建逻辑。
- **不接入新增的云服务域**:`spark/v1`(妙搭)、`bot/v4`、`okr/v2`、`elearning/v2`、`performance/v1`、`trust_party/v1`、`unified_kms/v1`、`application/v5`+`v7`、`auth/v4`,以及 `drive/v1` 新增的文件评论/版本/导出接口。本项目的功能面用不到。
- **不订阅新增事件**。v3.12.0 新增了 `P2ChatMemberBotAddedV1`、`P2ChatAccessEventBotP2pChatEnteredV1`、`P2ChatUpdatedV1`、`P2ChatDisbandedV1`、`P2BotMenuV6` 等一批与机器人生命周期相关的事件,但按 `AGENTS.md` 的硬规则,**订阅的事件必须配套 handler,no-op handler 不算处理**。这些属于产品功能决策,不在本次升级范围。若后续要用,需同步更新 `internal/feishu/events.go` 与 `docs/feishu-permissions.md`。
- **不使用 `core/accesstoken`**(用户 OAuth 授权码/刷新令牌流程)与 client assertion 模式(密钥对替代 app secret)。当前是 tenant token 模式,无需求。

## 六、风险与验证(D6、D10)

### 6.1 单元测试覆盖不到的部分

现有测试是 HTTP 打桩,以下行为**必须靠真实环境验证**:

| 验证项 | 说明 |
| --- | --- |
| token 过期自愈 | 核心回归项。需构造真实失效 token,确认刷新后自动恢复,而非持续 99991663 |
| **多应用隔离** | P0 的核心风险点。多 frontend 场景下让一个应用的 token 失效,确认其余应用的 token **未被清除**,且无 token 拉取风暴 |
| 长连接断线重连 | 换用 SDK ws 客户端后的行为,含静默断连(网络假死但 TCP 未断)场景 |
| **休眠/唤醒恢复** | 验证 D7 的残留风险是否成立(3.4.4)。合盖休眠数分钟后唤醒,确认连接自行恢复收消息、且耗时可接受;若出现"长期不恢复"即为该风险已发生,需回头改 D7 |
| 卡片回调与 channel 共存 | 守护 3.4.2:未调用 `OnCardAction` 时卡片回调正常触发,toast 与卡片更新不受影响 |
| 进程优雅退出 | `CloseAndWait` 后无 goroutine 泄漏、无消息丢失 |
| `feishu new` 全流程 | 扫码建应用、Lark 域名自动切换、超时与拒绝分支 |
| 卡片交互与消息收发 | 升级后的常规回归,确认无接口行为漂移。重点覆盖 toast 回写(456 处路径,见 3.2.2)与卡片 patch |

### 6.2 验证策略(D6:全量单测 + 关键三项真实环境验证)

**第一部分:全量单测。** 含本次新增的回归用例——P0 的多应用隔离、P2 的 `InboundMessage` 解析、channel 与自持卡片回调共存。

**第二部分:以下三项在真实租户验证。** 选这三项的理由是它们各自守护一个"单测无法覆盖且出错代价最高"的点:

| 验证项 | 守护什么 | 具体做法 |
| --- | --- | --- |
| token 过期自愈 **+ 多应用隔离** | P0 的核心风险;升级引入的行为破坏点 | 同时构造失效 token 与多 frontend 场景,确认单应用刷新不会清掉其余应用的 token,且无拉取风暴 |
| 休眠/唤醒恢复 | D7 的残留风险(3.4.4) | 合盖休眠数分钟后唤醒,确认连接自行恢复收消息且耗时可接受;若出现长期不恢复,即该风险已发生,需回头改 D7 |
| 卡片回调与 channel 共存 | 3.4.2 的核心约束 | 实际点击卡片按钮,确认 toast 正常弹出、卡片正常更新 |

**其余四项**(长连接断线重连、进程优雅退出、`feishu new` 全流程、常规回归)靠单测覆盖 + 上线后观察,不单独安排灰度。

**发布窗口(D10)**:本次安排在**独立窗口**发布,避开其他大改动同期——P2 同时替换了长连接与事件层,一旦出问题难归因,独立发布便于定位与回滚。

## 七、定案状态

**本文档涉及的全部决策已定案**,见第三节决策表 D1–D10。

**实现进度:P0–P4 均已落地**(2026-09-30)。`go build` / `go vet` 干净,全量测试 1132 通过。6.2 的三项真实环境验证待排期。

定案后形成的边界与例外集中记录于以下五处,实现时以此为准:

| 位置 | 内容 |
| --- | --- |
| 3.1.3 | token 清除判据:按 `:` 切分取 appID 段,配套金丝雀 |
| 3.4.7 | channel 采用边界:尽量用,发送路径与卡片回调除外 |
| 3.5.0 | 完整删除清单(不止 `ws_runtime.go`,含 `adapter.go` 中的散落部分) |
| 3.5.4 | 明确接受的能力降级:③ 项中只有 ① 可恢复,决定不补回(D8) |
| 6.2 | 验证策略与发布窗口(D6、D10) |

**实现时必须落实的三条动作**(易漏,单列):

1. **撤掉 `events.go` 中三个与 channel 重叠的事件注册**(P2 第 3 条)——否则 dispatcher 重复注册 panic;
2. **显式 `WithWriteTimeout(15*time.Second)`**(P2 第 7 条)——否则写超时从 15s 静默变为 10s;
3. **`Start` 保留 endpoint 预检但去掉拨号**(P2 第 11 条)——否则 daemon 会在连不上飞书时报告启动成功。

尚未消除、作为已知风险接受的项:

- **半死连接最长 245 秒才被发现**(3.5.4,D8)——由 6.2 的"休眠/唤醒恢复"实测项一并观察;
- **服务端配置可能被无保护覆盖为零**(3.5.5)——依赖预检中的金丝雀日志发现;
- **若 SDK 变更缓存 key 分隔符或卡片回调签名**,相关结论需重新评估(3.1.4 金丝雀、D4 注)。
