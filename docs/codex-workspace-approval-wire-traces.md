# Codex Workspace Approval Wire Traces

本文档单独记录两次稳定复现的 wire 级观测结果，和调查结论文档分开。

调查结论文档见：

- [docs/codex-stdio-cwd-probe-payloads.md](/home/yuhuan/feidex/docs/codex-stdio-cwd-probe-payloads.md)

相关测试代码：

- [internal/codexrpc/integration_live_ws_cwd_test.go](/home/yuhuan/feidex/internal/codexrpc/integration_live_ws_cwd_test.go)

## 1. 采集粒度

当前 harness 能稳定拿到这些信息：

- 出站 `initialize` payload 形状
- 出站 `thread/start` payload 形状
- 出站 `turn/start` payload 形状
- 入站 server request 的 `method`
- 入站 `item/fileChange/requestApproval` 的完整 `params`
- 关键通知的到达顺序

当前 harness 没有为这两个 case 逐条落盘所有 notification 的完整原始 JSON，所以这里的“wire trace”是：

- 真实请求 payload
- 真实 approval payload
- 真实 notification sequence

而不是“完整 JSON-RPC 帧转储”。

## 2. 共享出站 payload

两次复现共享同一组核心出站 payload。

### 2.1 `initialize`

```json
{
  "clientInfo": {
    "name": "feidex",
    "title": "Feidex Feishu Middleware",
    "version": "0.1.0"
  },
  "capabilities": {
    "experimentalApi": true,
    "optOutNotificationMethods": [
      "item/agentMessage/delta",
      "item/plan/delta",
      "item/commandExecution/outputDelta",
      "item/fileChange/outputDelta",
      "item/reasoning/summaryTextDelta",
      "item/reasoning/summaryPartAdded",
      "item/reasoning/textDelta"
    ]
  }
}
```

随后：

```json
{
  "method": "initialized",
  "params": {}
}
```

### 2.2 `thread/start`

```json
{
  "cwd": "<workspace B>",
  "approvalPolicy": "on-request",
  "sandbox": "workspace-write",
  "serviceName": "feidex-integration",
  "experimentalRawEvents": false,
  "persistExtendedHistory": true
}
```

### 2.3 `turn/start`

```json
{
  "threadId": "<thread-id>",
  "cwd": "<workspace B>",
  "approvalPolicy": "on-request",
  "sandboxPolicy": {
    "type": "workspaceWrite"
  },
  "input": [
    {
      "type": "text",
      "text": "You must edit calc.go so Add returns subtraction (`a - b`) instead of addition. Change only that file. If approval is required, use the built-in approval request mechanism and wait. After the edit finishes, reply with exactly FILE_OK.",
      "text_elements": []
    }
  ]
}
```

## 3. Trace A: `stdio` reproduction under `/home/yuhuan`

测试名：

- `TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFileApproval`

真实运行命令：

```bash
FEIDEX_CODEX_COMMAND=codex \
FEIDEX_CODEX_RUN_TOKEN_TESTS=1 \
FEIDEX_CODEX_PROBE_WORKSPACE_DIR=/home/yuhuan/.codex-probes/workspace-b.3sRvO5 \
./codexrpc_integration.test -test.run TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFileApproval -test.v
```

运行目录：

- 进程 cwd `A` = `/home/yuhuan/.codex-probes/codex-cwd-a.Brvex7`
- workspace `B` = `/home/yuhuan/.codex-probes/workspace-b.3sRvO5`

### 3.1 关键标识

```text
threadId = 019d9dee-9098-7d70-8dea-09748b279f63
turnId   = 019d9dee-9196-75d2-aeee-ed8e989763dc
```

### 3.2 实际抓到的 server request

请求方法列表：

```json
[
  "item/fileChange/requestApproval"
]
```

`item/fileChange/requestApproval` payload：

```json
{
  "threadId": "019d9dee-9098-7d70-8dea-09748b279f63",
  "turnId": "019d9dee-9196-75d2-aeee-ed8e989763dc",
  "itemId": "call_QQJHIoy88jy3ife0Jh21BDG9",
  "reason": "command failed; retry without sandbox?",
  "grantRoot": null
}
```

### 3.3 实际抓到的 notification sequence

```text
thread/status/changed
-> turn/started(019d9dee-9196-75d2-aeee-ed8e989763dc)
-> item/started(userMessage)
-> item/completed(userMessage)
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> item/started(fileChange)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> thread/status/changed
-> serverRequest/resolved
-> thread/status/changed
-> item/completed(fileChange)
-> turn/diff/updated
-> turn/diff/updated
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> turn/diff/updated
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> turn/diff/updated
-> thread/status/changed
-> turn/completed(019d9dee-9196-75d2-aeee-ed8e989763dc,completed)
```

### 3.4 直接观察

- approval 出现在 `item/started(fileChange)` 之后
- `serverRequest/resolved` 出现在 `item/completed(fileChange)` 之前
- approval reason 不是直接说“file not writable”，而是 `command failed; retry without sandbox?`

## 4. Trace B: `ws` reproduction under `/home/yuhuan`

测试名：

- `TestLiveCodexWebSocketExplicitProcessCWDDoesNotTriggerWorkspaceWriteFileApproval`

真实运行命令：

```bash
FEIDEX_CODEX_COMMAND=codex \
FEIDEX_CODEX_RUN_TOKEN_TESTS=1 \
FEIDEX_CODEX_PROBE_PROCESS_CWD_DIR=/home/yuhuan/.codex-probes/ws-cwd-a.3tmZ75 \
FEIDEX_CODEX_PROBE_WORKSPACE_DIR=/home/yuhuan/.codex-probes/ws-workspace-b.DpUM4K \
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexWebSocketExplicitProcessCWDDoesNotTriggerWorkspaceWriteFileApproval -v
```

运行目录：

- 进程 cwd `A` = `/home/yuhuan/.codex-probes/ws-cwd-a.3tmZ75`
- workspace `B` = `/home/yuhuan/.codex-probes/ws-workspace-b.DpUM4K`

### 4.1 关键标识

```text
threadId = 019d9df4-e4f3-7e41-9cc4-75bd5ad10b11
turnId   = 019d9df4-e5ff-7ce2-8c27-92ed8d2bf382
```

### 4.2 实际抓到的 server request

请求方法列表：

```json
[
  "item/fileChange/requestApproval"
]
```

`item/fileChange/requestApproval` payload：

```json
{
  "threadId": "019d9df4-e4f3-7e41-9cc4-75bd5ad10b11",
  "turnId": "019d9df4-e5ff-7ce2-8c27-92ed8d2bf382",
  "itemId": "call_Pop8cbVwLDPMWLxvpBlWhIw1",
  "reason": "command failed; retry without sandbox?",
  "grantRoot": null
}
```

### 4.3 实际抓到的 notification sequence

```text
thread/started
-> thread/status/changed
-> turn/started(019d9df4-e5ff-7ce2-8c27-92ed8d2bf382)
-> item/started(userMessage)
-> item/completed(userMessage)
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> item/started(fileChange)
-> thread/status/changed
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> serverRequest/resolved
-> thread/status/changed
-> item/completed(fileChange)
-> turn/diff/updated
-> turn/diff/updated
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> item/started(commandExecution)
-> item/completed(commandExecution)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> turn/diff/updated
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> item/started(reasoning)
-> item/completed(reasoning)
-> item/started(agentMessage)
-> item/completed(agentMessage)
-> thread/tokenUsage/updated
-> account/rateLimits/updated
-> turn/diff/updated
-> thread/status/changed
-> turn/completed(019d9df4-e5ff-7ce2-8c27-92ed8d2bf382,completed)
```

### 4.4 直接观察

- `ws` 版本的 approval payload 和 `stdio` 版本同构
- 一样是在 `item/started(fileChange)` 之后触发
- 一样是 `serverRequest/resolved` 先于 `item/completed(fileChange)`

## 5. 两条 trace 的共同模式

### 5.1 共同点

- 都是 `A` 和 `B` 分离
- 都是 `A/B` 落在 `/home/yuhuan` 而不是 `/tmp`
- 都是 `thread/start.cwd = B`
- 都是 `turn/start.cwd = B`
- 都没有 `writableRoots`
- 都触发了同一个方法：

```json
[
  "item/fileChange/requestApproval"
]
```

- 都给出同一个 reason：

```text
command failed; retry without sandbox?
```

### 5.2 差异点

- `ws` trace 多了开头的 `thread/started`
- 两边 commandExecution / tokenUsage 的穿插节奏略有不同
- 但 approval 的位置和语义没有本质差异

## 6. 直接可引用的判断

如果后续要对外提 issue，可以直接引用这几条：

- 这不是单纯的 `stdio` 问题，因为 `ws` 也复现
- 这不是“最终写错目录”的问题，因为代码改动确实落在 `B`
- 这也不是 `/tmp` 下的普通问题，因为 `/tmp` 会给出假阴性
- 当前最接近的现象是：Codex 在 `A/B` 分离时，某些内部操作仍然受进程 cwd 或 session cwd 影响，从而把 workspace 内正常 file change 升级成 approval
