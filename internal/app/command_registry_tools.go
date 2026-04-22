package app

import "feidex/internal/feishu"

func localCommandCommonToolSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/history"},
			IsLocal: func(fields []string) bool {
				return matchHistoryCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandHistory(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/history", Summary: "查看当前 thread 的 turn 历史记录，重点展示每个 turn 的输入。"},
				{Command: "/history detail TURN_NUMBER", Summary: "直接查看指定 Turn # 的详情。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(matchHistoryCommand, backendConversationHelpEntries(backendClaude, []helpCommandSpec{
				{Command: "/history", Summary: "查看当前 session 的 turn 历史记录，重点展示每个 turn 的输入。"},
				{Command: "/history detail TURN_NUMBER", Summary: "直接查看指定 Turn # 的详情。"},
			}))),
		},
		{
			Names: []string{"/skills"},
			IsLocal: func(fields []string) bool {
				return matchSkillsCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandSkills(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/skills", Summary: "查看当前工作区可用的 skills，并选择下一条消息默认携带的 skill。"},
				{Command: "/skills reload", Summary: "强制刷新当前工作区的 skill 列表。"},
				{Command: "$skill-name <内容>", Summary: "以 skill 前缀显式指定本条消息使用的 skill。"},
			},
			Backends: backendPoliciesForUnsupportedFeature(backendFeatureSkills),
		},
		{
			Names: []string{"/usage"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandUsage(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/usage", Summary: "查看当前 thread 的累计 token usage。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(exactCommand, backendConversationHelpEntries(backendClaude, []helpCommandSpec{
				{Command: "/usage", Summary: "查看当前 session 的累计 token usage。"},
			}))),
		},
	}
}

func localCommandWorkspaceToolSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/download"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandDownload(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/download", Summary: "在当前 workspace 范围内选择文件，并生成下载链接。"},
			},
		},
		{
			Names: []string{"/review"},
			IsLocal: func(fields []string) bool {
				return matchReviewCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandReview(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/review", Summary: "审查当前 workspace 的未提交改动。"},
				{Command: "/review uncommitted", Summary: "等价于 `/review`。"},
				{Command: "/review base", Summary: "打开 base branch 选择卡。"},
				{Command: "/review base <branch>", Summary: "对比指定 branch 发起 review。"},
				{Command: "/review commit", Summary: "打开最近 100 个 commit 选择卡。"},
				{Command: "/review commit <rev>", Summary: "审查指定 commit/ref。"},
				{Command: "/review custom", Summary: "打开自定义 review instructions 卡。"},
				{Command: "/review custom <instructions>", Summary: "按自定义 instructions 发起 review。"},
			},
			Backends: backendPoliciesForUnsupportedFeature(backendFeatureReview),
		},
	}
}
