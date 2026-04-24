package app

import "feidex/internal/feishu"

func localCommandModelSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/model"},
			IsLocal: func(fields []string) bool {
				return matchModelCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newModelConfigService(a).commandModel(msg, args)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/model", Summary: "打开模型选择与推理强度配置。"},
				{Command: "/model set <model-id>", Summary: "直接设置全局 model。"},
				{Command: "/model set default", Summary: "恢复全局默认 model。"},
				{Command: "/model effort <effort>", Summary: "直接设置全局推理强度。"},
				{Command: "/model effort default", Summary: "恢复默认推理强度。"},
			},
		},
		{
			Names: []string{"/effort"},
			IsLocal: func(fields []string) bool {
				return matchEffortCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newModelConfigService(a).commandEffort(msg, args)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/effort", Summary: "打开模型与推理强度配置。"},
				{Command: "/effort <effort>", Summary: "直接设置全局推理强度。"},
				{Command: "/effort default", Summary: "恢复默认推理强度。"},
			},
		},
		{
			Names: []string{"/quiet"},
			IsLocal: func(fields []string) bool {
				return exactOrSingleArgCommand(fields, "config", "verbose", "progress", "normal", "final")
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return commandQuiet(a,msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/quiet", Summary: "打开 Quiet Mode 配置卡。"},
				{Command: "/quiet verbose", Summary: "完整展开所有过程消息。"},
				{Command: "/quiet progress", Summary: "把过程消息折叠成“工作中”卡。"},
				{Command: "/quiet normal", Summary: "只保留 plan 和 agent/final message。"},
				{Command: "/quiet final", Summary: "只保留 final message。"},
				{Command: "/quiet config", Summary: "打开 Quiet Mode 配置卡。"},
			},
		},
	}
}

func localCommandFastSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/fast"},
			IsLocal: func(fields []string) bool {
				return exactOrSingleArgCommand(fields, "config", "fast", "default", "off", "toggle")
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return commandFast(a, msg, args)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/fast", Summary: "切换当前线程的响应速度设置。"},
				{Command: "/fast fast", Summary: "将当前线程的 service tier 设为 fast。"},
				{Command: "/fast default", Summary: "将当前线程的 service tier 恢复为默认。"},
				{Command: "/fast toggle", Summary: "切换当前线程的响应速度设置。"},
				{Command: "/fast config", Summary: "打开当前线程的响应速度配置卡。"},
			},
			Backends: backendPoliciesForUnsupportedFeature(backendFeatureFastMode),
		},
	}
}
