package app

import "feidex/internal/feishu"

func claudeWorkspaceHelpEntries() []helpCommandSpec {
	return []helpCommandSpec{
		{Command: "/workspace", Summary: "打开工作区菜单。"},
		{Command: "/workspace list", Summary: "打开工作区列表并可直接切换。"},
		{Command: "/workspace new", Summary: "创建新工作区。"},
		{Command: "/workspace clone GIT_URL [ID] [--parent DIR]", Summary: "从 Git 仓库创建新工作区，可显式指定父目录。"},
		{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
		{Command: "/workspace delete", Summary: "打开工作区删除菜单。"},
		{Command: "/workspace delete ID", Summary: "删除指定工作区的配置，不删除磁盘目录。"},
		{Command: "/workspace permissions", Summary: "配置当前工作区默认 Claude 权限模式。"},
		{Command: "/workspace permissions MODE", Summary: "直接设置当前工作区默认 Claude 权限模式。"},
		{Command: "/workspace permissions inherit", Summary: "清除工作区覆盖，恢复为跟随全局默认。"},
	}
}

func localCommandWorkspaceSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/workspace"},
			IsLocal: func(fields []string) bool {
				return matchWorkspaceCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newWorkspaceCommandService(a).commandWorkspace(msg, args)
			},
			HelpGroup: "workspace",
			HelpEntries: []helpCommandSpec{
				{Command: "/workspace", Summary: "打开工作区菜单。"},
				{Command: "/workspace list", Summary: "打开工作区列表并可直接切换。"},
				{Command: "/workspace new", Summary: "创建新工作区。"},
				{Command: "/workspace clone GIT_URL [ID] [--parent DIR]", Summary: "从 Git 仓库创建新工作区，可显式指定父目录。"},
				{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
				{Command: "/workspace delete", Summary: "打开工作区删除菜单。"},
				{Command: "/workspace delete ID", Summary: "删除指定工作区的配置，不删除磁盘目录。"},
				{Command: "/workspace sandbox", Summary: "配置当前工作区默认 sandbox。"},
				{Command: "/workspace sandbox MODE", Summary: "直接设置当前工作区默认 sandbox。"},
				{Command: "/workspace policy", Summary: "配置当前工作区默认 approval policy。"},
				{Command: "/workspace policy POLICY", Summary: "直接设置当前工作区默认 approval policy。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(matchClaudeWorkspaceCommand, claudeWorkspaceHelpEntries())),
		},
	}
}
