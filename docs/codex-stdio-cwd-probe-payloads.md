# Codex Workspace Approval Investigation

本文档整理当前已经确认的结论，目标是回答这几个问题：

- 当 `codex app-server` 进程 cwd 在目录 `A`，而 `thread/start.cwd` / `turn/start.cwd` 在独立目录 `B` 时，为什么 workspace 内正常代码编辑还会触发 `item/fileChange/requestApproval`？
- 这个问题是否只发生在 `stdio`，还是 `ws` 也会发生？
- `/tmp` 下为什么一开始复现不出来？
- `openai/codex` 上游有没有接近的问题？

相关测试文件：

- [internal/codexrpc/integration_live_ws_cwd_test.go](/home/yuhuan/feidex/internal/codexrpc/integration_live_ws_cwd_test.go)
- [internal/codexrpc/integration_live_state_machine_test.go](/home/yuhuan/feidex/internal/codexrpc/integration_live_state_machine_test.go)
- [internal/codexrpc/integration_live_review_test.go](/home/yuhuan/feidex/internal/codexrpc/integration_live_review_test.go)

wire trace 文档：

- [docs/codex-workspace-approval-wire-traces.md](/home/yuhuan/feidex/docs/codex-workspace-approval-wire-traces.md)

## 1. 固定实验条件

后面所有关键实验都以这组条件为准：

- `codexrpc.Client.Command = "codex"`
- 靠 `PATH` 启动 `codex app-server`
- 不使用 wrapper
- 不设置 `AppServerDir`
- 不设置 `TMPDIR`
- 启动进程的实际 cwd 是 `A`
- workspace 目录是独立的 `B`
- `A` 和 `B` 不互相包含
- `thread/start` 不传 `writableRoots`
- `turn/start` 不传 `writableRoots`
- `turn/start.sandboxPolicy = {"type":"workspaceWrite"}`

发给 `codexrpc.New` 的配置形状：

```json
{
  "Command": "codex",
  "Transport": "<stdio-or-ws>",
  "ExperimentalAPI": true,
  "ServiceName": "feidex-integration"
}
```

`stdio` 启动代码路径：

```go
c.cmd = exec.CommandContext(ctx, c.cfg.Command, "app-server")
```

来源：

- [client.go](/home/yuhuan/feidex/internal/codexrpc/client.go)

## 2. 实际发送的 payload

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

随后发送：

```json
{
  "method": "initialized",
  "params": {}
}
```

### 2.2 `thread/start`

```json
{
  "cwd": "<B>",
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
  "cwd": "<B>",
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

关键点：

- `cwd` 明确是 `B`
- 没有 `writableRoots`
- 没有把 `A` 当成 workspace 传给 Codex

## 3. 复现实验矩阵

### 3.1 `/tmp` 下的 direct `codex` probe

测试名：

- `TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFileApproval`

真实运行方式：

- 测试二进制直接放到 `/tmp/codex-cwd-a.*`
- 测试进程 cwd 就是该目录
- workspace `B` 也落在 `/tmp/...`

一次真实结果：

```text
process_cwd=/tmp/codex-cwd-a.KVRPoc workspace=/tmp/TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFile2193635741/001/workspace-b request_methods=[] file_approval_count=0
PASS
```

结论：

- 在 `/tmp` 下，这个问题没有复现
- 这是一个假阴性环境

### 3.2 `/home/yuhuan` 下的 direct `codex` `stdio` probe

测试名：

- `TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFileApproval`

真实运行方式：

```bash
FEIDEX_CODEX_COMMAND=codex \
FEIDEX_CODEX_RUN_TOKEN_TESTS=1 \
FEIDEX_CODEX_PROBE_WORKSPACE_DIR=/home/yuhuan/.codex-probes/workspace-b.3sRvO5 \
./codexrpc_integration.test -test.run TestLiveCodexInheritedProcessCWDDoesNotTriggerWorkspaceWriteFileApproval -test.v
```

运行时目录：

- `A = /home/yuhuan/.codex-probes/codex-cwd-a.Brvex7`
- `B = /home/yuhuan/.codex-probes/workspace-b.3sRvO5`

真实结果：

```text
process_cwd=/home/yuhuan/.codex-probes/codex-cwd-a.Brvex7 workspace=/home/yuhuan/.codex-probes/workspace-b.3sRvO5 request_methods=[item/fileChange/requestApproval] file_approval_count=1
```

抓到的 approval payload：

```json
{
  "threadId": "019d9dee-9098-7d70-8dea-09748b279f63",
  "turnId": "019d9dee-9196-75d2-aeee-ed8e989763dc",
  "itemId": "call_QQJHIoy88jy3ife0Jh21BDG9",
  "reason": "command failed; retry without sandbox?",
  "grantRoot": null
}
```

跑后文件状态：

- `B/calc.go` 被改成了减法
- `B` 里新增了空文件 `.codex`
- `A` 里也新增了空文件 `.codex`
- `A` 没有生成 `calc.go`

`git status` 结果：

```text
 M calc.go
?? .codex
```

结论：

- `stdio` 在 `/home/yuhuan` 下可以稳定触发 `item/fileChange/requestApproval`
- 问题不是“写错目录”，因为真正代码改动仍然落在 `B`
- 但启动目录 `A` 仍然被 Codex 写入了 `.codex`

### 3.3 `/home/yuhuan` 下的 `ws` probe

测试名：

- `TestLiveCodexWebSocketExplicitProcessCWDDoesNotTriggerWorkspaceWriteFileApproval`

真实运行方式：

```bash
FEIDEX_CODEX_COMMAND=codex \
FEIDEX_CODEX_RUN_TOKEN_TESTS=1 \
FEIDEX_CODEX_PROBE_PROCESS_CWD_DIR=/home/yuhuan/.codex-probes/ws-cwd-a.3tmZ75 \
FEIDEX_CODEX_PROBE_WORKSPACE_DIR=/home/yuhuan/.codex-probes/ws-workspace-b.DpUM4K \
./scripts/with_tmp_go_cache.sh go test -count=1 -tags=integration ./internal/codexrpc -run TestLiveCodexWebSocketExplicitProcessCWDDoesNotTriggerWorkspaceWriteFileApproval -v
```

运行时目录：

- `A = /home/yuhuan/.codex-probes/ws-cwd-a.3tmZ75`
- `B = /home/yuhuan/.codex-probes/ws-workspace-b.DpUM4K`

真实结果：

```text
ws_process_cwd=/home/yuhuan/.codex-probes/ws-cwd-a.3tmZ75 workspace=/home/yuhuan/.codex-probes/ws-workspace-b.DpUM4K request_methods=[item/fileChange/requestApproval] file_approval_count=1
```

抓到的 approval payload：

```json
{
  "threadId": "019d9df4-e4f3-7e41-9cc4-75bd5ad10b11",
  "turnId": "019d9df4-e5ff-7ce2-8c27-92ed8d2bf382",
  "itemId": "call_Pop8cbVwLDPMWLxvpBlWhIw1",
  "reason": "command failed; retry without sandbox?",
  "grantRoot": null
}
```

跑后文件状态：

- `B/calc.go` 被改成了减法
- `B` 里新增了空文件 `.codex`
- `A` 里也新增了空文件 `.codex`
- `A` 没有生成 `calc.go`

`git status` 结果：

```text
 M calc.go
?? .codex
```

结论：

- `ws` 在同样的 `A/B` 条件下也会触发同样的 approval
- 所以这个问题不是 `stdio` 独有

## 4. 当前已经确认的结论

### 4.1 `/tmp` 会掩盖问题

当前最重要的实验结论是：

- 把 `A/B` 放在 `/tmp` 下，问题可能不出现
- 把 `A/B` 放在 `/home/yuhuan` 这类正常用户目录下，问题就稳定出现

所以：

- `/tmp` 不是可靠的复现环境
- 很可能被 Codex 当成默认可写或特殊处理路径

### 4.2 问题不是单纯的 `stdio` transport 问题

因为：

- `stdio` 复现了
- `ws` 也复现了

所以：

- 单靠“把 Feidex 的 `stdio` app-server cwd 绑到 workspace”并不能解释全部问题
- 如果 Feidex 的 `ws` 模式也让 app-server 进程 cwd 落在错误目录，理论上也会撞上同类问题

### 4.3 问题更像“进程 cwd / session cwd / workspace cwd 混淆”

现象组合是：

- `thread/start.cwd = B`
- `turn/start.cwd = B`
- 真实代码编辑也发生在 `B`
- 但 `A` 和 `B` 都被写入了 `.codex`
- 最终仍然出现了 `item/fileChange/requestApproval`
- approval reason 是 `command failed; retry without sandbox?`

这更像是：

- 某些内部写操作或前置命令仍然依赖进程 cwd / session cwd
- 或者 sandbox 可写边界判断没有完全以请求里的 workspace `cwd` 为准

## 5. 与上游 `openai/codex` issue 的对应关系

没有找到和这次 case 完全一模一样的公开 issue，但有几个很接近：

- [#3749 apply_patch writes to session CWD instead of requested workspace](https://github.com/openai/codex/issues/3749)
  - 最接近当前问题
  - 指向“写操作用了 session CWD，而不是请求的 workspace”

- [#3140 all commands in sandbox mode fail and need approval](https://github.com/openai/codex/issues/3140)
  - 和本次 approval reason 很接近
  - 当前两次复现里抓到的 `reason` 都是 `command failed; retry without sandbox?`

- [#5824 codex --sandbox workspace-write --ask-for-approval never cannot write files](https://github.com/openai/codex/issues/5824)
  - 说明 `workspace-write` 下本应可写的位置仍可能写失败

- [#7344 Codex keeps prompting for approval + excessive cd prepended shell commands](https://github.com/openai/codex/issues/7344)
  - 说明工作目录和 shell 命令前缀会影响 approval 判定

- [#5323 Investigate virtual workspace roots set by other extensions](https://github.com/openai/codex/issues/5323)
  - 说明 `workspace roots` / `writable_roots` 边界本身存在过问题

对应判断：

- 最接近当前根因方向的是 `#3749`
- 最接近当前直接症状的是 `#3140`
- 但“`A/B` 分离导致 workspace 内 file change 仍触发 approval”的精确 case，目前没找到公开 issue

## 6. 下一步建议

当前最值得继续做的有两项：

1. 把这次 `/home/yuhuan` 下 `stdio + ws` 的完整 wire payload 和通知序列单独落盘
2. 基于当前稳定复现条件，整理一份可以直接提交到 `openai/codex` 的 issue 草稿

如果继续在 Feidex 内验证，还要特别注意：

- 不能再用 `/tmp` 当默认复现路径
- `ws` 也必须纳入验证范围
- 不能只看“最终文件改成功了没有”，还要看是否额外触发了 `item/fileChange/requestApproval`
