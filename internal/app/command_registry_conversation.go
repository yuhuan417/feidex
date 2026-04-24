package app

import (
	"fmt"

	"feidex/internal/feishu"
)

func claudeSessionHelpEntries() []helpCommandSpec {
	return []helpCommandSpec{
		{Command: "/session", Summary: "打开 Claude 会话菜单。"},
		{Command: "/session list", Summary: "查看当前工作区可恢复的 Claude 会话。"},
		{Command: "/session list all", Summary: "查看更多来源的 Claude 会话。"},
		{Command: "/session new", Summary: "立即创建并切换到新的 Claude 会话。"},
		{Command: "/session fork", Summary: "基于当前 Claude 会话派生一个新分支，并立即切换过去。"},
		{Command: "/session resume SESSION_ID", Summary: "按 session id 直接恢复 Claude 会话。"},
		{Command: "/session permissions", Summary: "配置当前 Claude 会话的权限模式。"},
		{Command: "/session permissions MODE", Summary: "直接设置当前 Claude 会话的权限模式。"},
		{Command: "/session permissions inherit", Summary: "清除会话覆盖，恢复为跟随工作区。"},
	}
}

func localCommandConversationSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/compact"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return commandCompact(a, msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/compact", Summary: "压缩当前线程的上下文，减少上下文占用。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(exactCommand, backendConversationHelpEntries(backendClaude, []helpCommandSpec{
				{Command: "/compact", Summary: "压缩当前会话的上下文，减少上下文占用。"},
			}))),
		},
		{
			Names: []string{"/fork"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return commandFork(a, msg, args)
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/fork", Summary: "等价于 `/thread fork`。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(exactCommand, []helpCommandSpec{
				{Command: "/fork", Summary: "等价于 `/session fork`。"},
			})),
		},
		{
			Names: []string{"/new"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return newThreadService(a).commandThreadsNew(msg)
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/new", Summary: "等价于 `/thread new`。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(exactCommand, []helpCommandSpec{
				{Command: "/new", Summary: "等价于 `/session new`。"},
			})),
		},
		{
			Names: []string{"/thread"},
			IsLocal: func(fields []string) bool {
				return matchThreadCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newThreadService(a).commandThread(msg, args)
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/thread", Summary: "打开 thread 菜单。"},
				{Command: "/thread list", Summary: "查看当前工作区可恢复的线程。"},
				{Command: "/thread list all", Summary: "查看更多来源的线程，仅保留命令入口。"},
				{Command: "/thread new", Summary: "立即创建并切换到新的 thread。"},
				{Command: "/thread fork", Summary: "复制当前 thread 为一个新分支，并立即切换过去。"},
				{Command: "/thread resume THREAD_ID", Summary: "按 thread id 直接恢复线程。"},
				{Command: "/thread sandbox", Summary: "配置当前线程的 sandbox。"},
				{Command: "/thread sandbox MODE", Summary: "直接设置当前线程的 sandbox。"},
				{Command: "/thread policy", Summary: "配置当前线程的 approval policy。"},
				{Command: "/thread policy POLICY", Summary: "直接设置当前线程的 approval policy。"},
			},
			Backends: backendPoliciesForUnsupportedFeature(backendFeatureConversationThreadCommands),
		},
		{
			Names: []string{"/session"},
			IsLocal: func(fields []string) bool {
				return matchSessionCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newThreadService(a).commandSession(msg, args)
			},
			HelpGroup:   "thread",
			HelpEntries: claudeSessionHelpEntries(),
			Backends:    backendPoliciesForUnsupportedFeature(backendFeatureConversationSessionCommands),
		},
		{
			Names: []string{"/threads"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				if len(args) > 0 {
					return fmt.Errorf("usage: /threads")
				}
				return newThreadService(a).commandThread(msg, []string{"list"})
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/threads", Summary: "等价于 `/thread list`。"},
			},
			Backends: backendPoliciesForUnsupportedFeature(backendFeatureConversationThreadCommands),
		},
		{
			Names: []string{"/interrupt", "/stop"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return commandInterrupt(a, msg)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "`/interrupt` 或 `/stop`", Summary: "中断当前运行中的任务，并清空排队/暂存输入。"},
			},
		},
	}
}
