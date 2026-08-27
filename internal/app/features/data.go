package features

import (
	"strings"

	appruntime "feidex/internal/app/runtime"
)

var helpGroupOrder = []string{
	"常用工具",
	"model",
	"thread",
	"workspace",
	"system",
}

var registry = []Spec{
	{
		ID:       "menu.root",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "menu", Names: []string{"/menu"}, HelpEntries: []HelpCommandSpec{{Command: "/menu", Summary: "打开命令菜单。"}}}},
		Nodes:    []MenuNode{{Action: "menu.root", Label: "主菜单"}},
		ActionNames: []ActionName{
			"menu.root",
		},
	},
	{
		ID:   "menu.tools",
		Kind: SpecKindSection,
		Nodes: []MenuNode{
			{Action: "menu.tools", Label: "常用工具", Parent: "menu.root"},
		},
		MenuGroup: &MenuGroupSpec{Action: "menu.tools", Label: "常用工具", Description: "常用会话与线程工具入口。", ShowInRoot: true},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.root", Label: "返回上一级", Kind: MenuItemBack},
		},
		ActionNames: []ActionName{
			"menu.tools",
		},
	},
	{
		ID:   "menu.group.model",
		Kind: SpecKindSection,
		Nodes: []MenuNode{
			{Action: "menu.group.model", Label: "模型配置", Parent: "menu.root"},
		},
		MenuGroup: &MenuGroupSpec{Action: "menu.group.model", Label: "模型配置", Description: "模型选择与响应速度配置。", ShowInRoot: true},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.model", Action: "menu.root", Label: "返回上一级", Kind: MenuItemBack},
		},
		ActionNames: []ActionName{
			"menu.group.model",
		},
	},
	{
		ID:   "menu.current_bot",
		Kind: SpecKindSection,
		Commands: []CommandSpec{
			{ID: "primary", Names: []string{"/primary"}},
		},
		ActionNames: []ActionName{
			"menu.current_bot",
		},
	},
	{
		ID:   "menu.current_workspace",
		Kind: SpecKindSection,
		ActionNames: []ActionName{
			"menu.current_workspace",
			"current_workspace.choose",
			"current_workspace.use",
		},
	},
	{
		ID:       "menu.group.backend",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "backend", Names: []string{"/backend"}, HelpGroup: "system", HelpEntries: []HelpCommandSpec{{Command: "/backend", Summary: "查看本 frontend 当前可用的 backend，并在空闲态切换。"}, {Command: "/backend retry", Summary: "查看当前 frontend 的自动重试开关。"}, {Command: "/backend retry on", Summary: "开启当前 frontend 的自动重试。"}, {Command: "/backend retry off", Summary: "关闭当前 frontend 的自动重试。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.group.backend", Label: "后端管理", Parent: "menu.group.system"},
			{Action: "menu.backend.switch", Label: "切换后端", Parent: "menu.group.backend"},
			{Action: "menu.auto_retry", Label: "自动重试", Parent: "menu.group.backend"},
		},
		MenuGroup: &MenuGroupSpec{Action: "menu.group.backend", Label: "后端管理", Description: "后端切换与各 CLI 管理入口。", ShowInRoot: false},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.system", Action: "menu.group.backend", Label: "后端管理", Kind: MenuItemSubmenu},
			{GroupAction: "menu.group.backend", Action: "menu.backend.switch", Label: "切换后端", Slash: "/backend", Kind: MenuItemDirect},
			{GroupAction: "menu.group.backend", Action: "menu.auto_retry", Label: "自动重试", Slash: "/backend retry", Kind: MenuItemDirect},
			{GroupAction: "menu.group.backend", Action: "menu.group.system", Label: "返回上一级", Kind: MenuItemBack},
		},
		ActionNames: []ActionName{
			"menu.group.backend",
			"menu.backend",
			"menu.backend.switch",
			"menu.auto_retry",
			"backend.select",
			"auto_retry.set",
		},
	},
	{
		ID:       "menu.review",
		Kind:     SpecKindCapability,
		Backends: []string{appruntime.BackendCodex},
		Commands: []CommandSpec{{ID: "review", Names: []string{"/review"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/review", Summary: "审查当前 workspace 的未提交改动。"}, {Command: "/review uncommitted", Summary: "等价于 `/review`。"}, {Command: "/review base", Summary: "打开 base branch 选择卡。"}, {Command: "/review base <branch>", Summary: "对比指定 branch 发起 review。"}, {Command: "/review commit", Summary: "打开最近 100 个 commit 选择卡。"}, {Command: "/review commit <rev>", Summary: "审查指定 commit/ref。"}, {Command: "/review custom", Summary: "打开自定义 review instructions 卡。"}, {Command: "/review custom <instructions>", Summary: "按自定义 instructions 发起 review。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}}},
		Nodes: []MenuNode{
			{Action: "menu.review", Label: "代码审查", Parent: "menu.tools"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.review", Label: "代码审查", Slash: "/review", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.review",
			"menu.review.uncommitted",
			"menu.review.base",
			"menu.review.commit",
			"menu.review.custom",
		},
	},
	{
		ID:       "menu.quiet",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "quiet", Names: []string{"/quiet"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/quiet", Summary: "打开 Quiet Mode 配置卡。"}, {Command: "/quiet verbose", Summary: "完整展开所有过程消息。"}, {Command: "/quiet progress", Summary: "把过程消息折叠成“工作中”卡。"}, {Command: "/quiet normal", Summary: "只保留 plan 和 agent/final message。"}, {Command: "/quiet final", Summary: "只保留 final message。"}, {Command: "/quiet config", Summary: "打开 Quiet Mode 配置卡。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.quiet", Label: "静默模式", Parent: "menu.tools"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.quiet", Label: "静默模式", Slash: "/quiet config", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.quiet",
			"quiet.set",
		},
	},
	{
		ID:       "plan",
		Kind:     SpecKindCapability,
		Backends: []string{appruntime.BackendCodex},
		Commands: []CommandSpec{{ID: "plan", Names: []string{"/plan"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/plan", Summary: "切换当前 thread 的 `plan` collaboration mode。"}, {Command: "/plan on", Summary: "为当前 thread 开启 `plan` collaboration mode。"}, {Command: "/plan off", Summary: "关闭当前 thread 的 `plan` collaboration mode。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}}},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.plan", Label: "计划模式", Slash: "/plan", Kind: MenuItemDirect, IncludeParentAction: true},
		},
		ActionNames: []ActionName{
			"menu.plan",
		},
	},
	{
		ID:       "goal",
		Kind:     SpecKindCapability,
		Backends: []string{appruntime.BackendCodex},
		Commands: []CommandSpec{{ID: "goal", Names: []string{"/goal"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/goal", Summary: "查看或创建当前 thread 的长期任务目标。"}, {Command: "/goal <objective>", Summary: "设置长期任务目标。"}, {Command: "/goal pause", Summary: "暂停当前 goal。"}, {Command: "/goal resume", Summary: "恢复当前 goal。"}, {Command: "/goal clear", Summary: "清除当前 goal。"}, {Command: "/goal edit", Summary: "编辑当前 goal。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}}},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.goal", Label: "任务目标", Slash: "/goal", Kind: MenuItemDirect, IncludeParentAction: true},
		},
		ActionNames: []ActionName{
			"menu.goal",
			"goal.pause",
			"goal.resume",
			"goal.clear",
			"goal.edit",
			"goal.replace.confirm",
			"goal.replace.cancel",
			"goal.edit.submit",
		},
	},
	{
		ID:       "menu.compact",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "compact", Names: []string{"/compact"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/compact", Summary: "压缩当前线程的上下文，减少上下文占用。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/compact", Summary: "压缩当前会话的上下文，减少上下文占用。"}}}}}},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.compact", Label: "压缩上下文", Slash: "/compact", Kind: MenuItemDirect, IncludeParentAction: true},
		},
		ActionNames: []ActionName{
			"menu.compact",
		},
	},
	{
		ID:       "menu.download",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "download", Names: []string{"/download"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/download", Summary: "在当前 workspace 范围内选择文件，并生成下载链接。"}}}},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.download", Label: "下载文件", Slash: "/download", Kind: MenuItemDirect, IncludeParentAction: true},
		},
		ActionNames: []ActionName{
			"menu.download",
		},
	},
	{
		ID:       "menu.history",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "history", Names: []string{"/history"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/history", Summary: "查看当前 thread 的 turn 历史记录，重点展示每个 turn 的输入。"}, {Command: "/history detail TURN_NUMBER", Summary: "直接查看指定 Turn # 的详情。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/history", Summary: "查看当前 session 的 turn 历史记录，重点展示每个 turn 的输入。"}, {Command: "/history detail TURN_NUMBER", Summary: "直接查看指定 Turn # 的详情。"}}}}}},
		Nodes: []MenuNode{
			{Action: "menu.history", Label: "历史记录", Parent: "menu.tools"},
			{Action: "history.detail", Label: "Turn 详情", Parent: "menu.history"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.history", Label: "历史记录", Slash: "/history", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.history",
			"history.page",
			"history.detail",
			"history.detail.select",
		},
	},
	{
		ID:       "menu.skills",
		Kind:     SpecKindCapability,
		Backends: []string{appruntime.BackendCodex},
		Commands: []CommandSpec{{ID: "skills", Names: []string{"/skills"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/skills", Summary: "查看当前工作区可用的 skills，并选择下一条消息默认携带的 skill。"}, {Command: "/skills reload", Summary: "强制刷新当前工作区的 skill 列表。"}, {Command: "$skill-name <内容>", Summary: "以 skill 前缀显式指定本条消息使用的 skill。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}}},
		Nodes: []MenuNode{
			{Action: "menu.skills", Label: "技能列表", Parent: "menu.tools"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.skills", Label: "技能列表", Slash: "/skills", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.skills",
			"skills.select",
			"skills.reload",
		},
	},
	{
		ID:       "menu.usage",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "usage", Names: []string{"/usage"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "/usage", Summary: "查看当前 thread 的累计 token usage。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/usage", Summary: "查看当前 session 的累计 token usage。"}}}}}},
		Nodes: []MenuNode{
			{Action: "menu.usage", Label: "Token 消耗", Parent: "menu.tools"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.usage", Label: "Token 消耗", Slash: "/usage", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.usage",
		},
	},
	{
		ID:       "menu.interrupt",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "interrupt", Names: []string{"/interrupt", "/stop"}, HelpGroup: "常用工具", HelpEntries: []HelpCommandSpec{{Command: "`/interrupt` 或 `/stop`", Summary: "中断当前运行中的任务，并清空排队/暂存输入。"}}}},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.tools", Action: "menu.interrupt", Label: "中断任务", Slash: "/stop", Kind: MenuItemDirect, IncludeParentAction: true},
		},
		ActionNames: []ActionName{
			"menu.interrupt",
		},
	},
	{
		ID:   "menu.thread",
		Kind: SpecKindCapability,
		Commands: []CommandSpec{
			{ID: "fork", Names: []string{"/fork"}, HelpGroup: "thread", HelpEntries: []HelpCommandSpec{{Command: "/fork", Summary: "等价于 `/thread fork`。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/fork", Summary: "等价于 `/session fork`。"}}}}},
			{ID: "new", Names: []string{"/new"}, HelpGroup: "thread", HelpEntries: []HelpCommandSpec{{Command: "/new", Summary: "等价于 `/thread new`。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/new", Summary: "等价于 `/session new`。"}}}}},
			{ID: "thread", Names: []string{"/thread"}, HelpGroup: "thread", HelpEntries: []HelpCommandSpec{{Command: "/thread", Summary: "打开 thread 菜单。"}, {Command: "/thread list", Summary: "查看当前工作区可恢复的线程。"}, {Command: "/thread list all", Summary: "查看更多来源的线程，仅保留命令入口。"}, {Command: "/thread new", Summary: "立即创建并切换到新的 thread。"}, {Command: "/thread fork", Summary: "复制当前 thread 为一个新分支，并立即切换过去。"}, {Command: "/thread resume THREAD_ID", Summary: "按 thread id 直接恢复线程。"}, {Command: "/thread sandbox", Summary: "配置当前线程的 sandbox。"}, {Command: "/thread sandbox MODE", Summary: "直接设置当前线程的 sandbox。"}, {Command: "/thread policy", Summary: "配置当前线程的 approval policy。"}, {Command: "/thread policy POLICY", Summary: "直接设置当前线程的 approval policy。"}, {Command: "/thread multiagent", Summary: "配置当前线程的 multi-agent mode。"}, {Command: "/thread multiagent MODE", Summary: "直接设置当前线程的 multi-agent mode。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}},
			{ID: "session", Names: []string{"/session"}, HelpGroup: "thread", HelpEntries: []HelpCommandSpec{{Command: "/session", Summary: "打开 Claude 会话菜单。"}, {Command: "/session list", Summary: "查看当前工作区可恢复的 Claude 会话。"}, {Command: "/session list all", Summary: "查看更多来源的 Claude 会话。"}, {Command: "/session new", Summary: "立即创建并切换到新的 Claude 会话。"}, {Command: "/session fork", Summary: "基于当前 Claude 会话派生一个新分支，并立即切换过去。"}, {Command: "/session resume SESSION_ID", Summary: "按 session id 直接恢复 Claude 会话。"}, {Command: "/session permissions", Summary: "配置当前 Claude 会话的权限模式。"}, {Command: "/session permissions MODE", Summary: "直接设置当前 Claude 会话的权限模式。"}, {Command: "/session permissions inherit", Summary: "清除会话覆盖，恢复为跟随工作区。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendCodex: {HideInHelp: true}}},
			{ID: "threads", Names: []string{"/threads"}, HelpGroup: "thread", HelpEntries: []HelpCommandSpec{{Command: "/threads", Summary: "等价于 `/thread list`。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}},
		},
		Nodes: []MenuNode{
			{Action: "menu.thread", Label: "线程管理", Parent: "menu.root"},
			{Action: "thread.sandbox.menu", Label: "线程沙箱", Parent: "menu.thread"},
			{Action: "thread.policy.menu", Label: "审批策略", Parent: "menu.thread"},
			{Action: "thread.multiagent.menu", Label: "多智能体模式", Parent: "menu.thread"},
			{Action: "thread.permission_mode.menu", Label: "会话权限", Parent: "menu.thread"},
		},
		MenuGroup: &MenuGroupSpec{Action: "menu.thread", Label: "线程管理", Description: "查看当前线程状态，并通过下拉切换线程。", ShowInRoot: true},
		ActionNames: []ActionName{
			"menu.thread",
			"menu.new",
			"menu.fork",
		},
	},
	{
		ID:   "menu.workspace",
		Kind: SpecKindCapability,
		Commands: []CommandSpec{
			{
				ID:        "workspace",
				Names:     []string{"/workspace"},
				HelpGroup: "workspace",
				HelpEntries: []HelpCommandSpec{
					{Command: "/workspace", Summary: "打开工作区菜单。"},
					{Command: "/workspace list", Summary: "打开工作区列表并可直接切换。"},
					{Command: "/workspace new", Summary: "创建新工作区。"},
					{Command: "/workspace new worktree [BRANCH] [ID]", Summary: "基于当前 Git 工作区创建 Worktree 工作区。"},
					{Command: "/workspace clone GIT_URL [ID] [--parent DIR]", Summary: "从 Git 仓库创建新工作区，表单中可选择 clone 后直接生成 worktree。"},
					{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
					{Command: "/workspace choose", Summary: "以按钮形式选择工作区，按最近使用排序。"},
					{Command: "/workspace delete", Summary: "打开工作区删除菜单。"},
					{Command: "/workspace delete ID", Summary: "删除指定工作区的配置，不删除磁盘目录。"},
					{Command: "/workspace sandbox", Summary: "配置当前工作区默认 sandbox。"},
					{Command: "/workspace sandbox MODE", Summary: "直接设置当前工作区默认 sandbox。"},
					{Command: "/workspace policy", Summary: "配置当前工作区默认 approval policy。"},
					{Command: "/workspace policy POLICY", Summary: "直接设置当前工作区默认 approval policy。"},
					{Command: "/workspace multiagent", Summary: "配置当前工作区默认 multi-agent mode。"},
					{Command: "/workspace multiagent MODE", Summary: "直接设置当前工作区默认 multi-agent mode。"},
				},
				Backends: map[string]CommandBackendSpec{
					appruntime.BackendClaude: {
						HelpEntries: []HelpCommandSpec{
							{Command: "/workspace", Summary: "打开工作区菜单。"},
							{Command: "/workspace list", Summary: "打开工作区列表并可直接切换。"},
							{Command: "/workspace new", Summary: "创建新工作区。"},
							{Command: "/workspace new worktree [BRANCH] [ID]", Summary: "基于当前 Git 工作区创建 Worktree 工作区。"},
							{Command: "/workspace clone GIT_URL [ID] [--parent DIR]", Summary: "从 Git 仓库创建新工作区，表单中可选择 clone 后直接生成 worktree。"},
							{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
							{Command: "/workspace choose", Summary: "以按钮形式选择工作区，按最近使用排序。"},
							{Command: "/workspace delete", Summary: "打开工作区删除菜单。"},
							{Command: "/workspace delete ID", Summary: "删除指定工作区的配置，不删除磁盘目录。"},
							{Command: "/workspace permissions", Summary: "配置当前工作区默认 Claude 权限模式。"},
							{Command: "/workspace permissions MODE", Summary: "直接设置当前工作区默认 Claude 权限模式。"},
							{Command: "/workspace permissions inherit", Summary: "清除工作区覆盖，恢复为跟随全局默认。"},
						},
					},
				},
			},
		},
		Nodes: []MenuNode{
			{Action: "menu.workspace", Label: "工作区管理", Parent: "menu.root"},
			{Action: "workspace.new", Label: "新建工作区", Parent: "menu.workspace"},
			{Action: "workspace.clone", Label: "从仓库创建", Parent: "menu.workspace"},
			{Action: "workspace.worktree", Label: "创建 Worktree", Parent: "menu.workspace"},
			{Action: "workspace.sandbox.menu", Label: "默认沙箱", Parent: "menu.workspace"},
			{Action: "workspace.policy.menu", Label: "默认策略", Parent: "menu.workspace"},
			{Action: "workspace.multiagent.menu", Label: "多智能体模式", Parent: "menu.workspace"},
			{Action: "workspace.permission_mode.menu", Label: "默认权限", Parent: "menu.workspace"},
			{Action: "workspace.delete.menu", Label: "删除工作区", Parent: "menu.workspace"},
			{Action: "workspace.delete.confirm", Label: "确认删除", Parent: "workspace.delete.menu"},
		},
		MenuGroup: &MenuGroupSpec{Action: "menu.workspace", Label: "工作区管理", Description: "查看当前工作区状态，并通过下拉切换工作区。", ShowInRoot: true},
		ActionNames: []ActionName{
			"menu.workspace",
		},
	},
	{
		ID:   "menu.group.system",
		Kind: SpecKindSection,
		Nodes: []MenuNode{
			{Action: "menu.group.system", Label: "系统运维", Parent: "menu.root"},
		},
		MenuGroup: &MenuGroupSpec{Action: "menu.group.system", Label: "系统运维", Description: "系统运维与帮助入口。", ShowInRoot: true},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.system", Action: "menu.root", Label: "返回上一级", Kind: MenuItemBack},
		},
		ActionNames: []ActionName{
			"menu.group.system",
		},
	},
	{
		ID:   "menu.model",
		Kind: SpecKindCapability,
		Commands: []CommandSpec{
			{ID: "model", Names: []string{"/model"}, HelpGroup: "model", HelpEntries: []HelpCommandSpec{{Command: "/model", Summary: "打开模型选择与推理强度配置。"}, {Command: "/model set <model-id>", Summary: "直接设置全局 model。"}, {Command: "/model set default", Summary: "恢复全局默认 model。"}, {Command: "/model effort <effort>", Summary: "直接设置全局推理强度。"}, {Command: "/model effort default", Summary: "恢复默认推理强度。"}, {Command: "/model plan", Summary: "查看 Plan 模式的独立模型配置。"}, {Command: "/model plan set <model-id>", Summary: "直接设置 Plan 模式模型。"}, {Command: "/model plan set default", Summary: "让 Plan 模式模型恢复跟随 default mode。"}, {Command: "/model plan effort <effort>", Summary: "直接设置 Plan 模式推理强度。"}, {Command: "/model plan effort default", Summary: "让 Plan 模式推理强度恢复跟随 plan preset。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/model", Summary: "打开模型选择与推理强度配置。"}, {Command: "/model set <model-id>", Summary: "直接设置默认 model。"}, {Command: "/model set default", Summary: "恢复默认 model。"}, {Command: "/model effort <effort>", Summary: "直接设置默认推理强度。"}, {Command: "/model effort default", Summary: "恢复默认推理强度。"}, {Command: "/model option add <model-id>", Summary: "把 model id 加入 Claude 模型选择下拉框。"}, {Command: "/model option remove <model-id>", Summary: "从 Claude 模型选择下拉框移除候选 model。"}}}}},
			{ID: "effort", Names: []string{"/effort"}, HelpGroup: "model", HelpEntries: []HelpCommandSpec{{Command: "/effort", Summary: "打开模型与推理强度配置。"}, {Command: "/effort <effort>", Summary: "直接设置全局推理强度。"}, {Command: "/effort default", Summary: "恢复默认推理强度。"}}},
		},
		Nodes: []MenuNode{
			{Action: "menu.model", Label: "模型配置", Parent: "menu.group.model"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.model", Action: "menu.model", Label: "模型配置", Slash: "/model", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.model",
			"model.config.set_model",
			"model.config.select_model",
			"model.config.add_option",
			"model.config.remove_option",
			"model.config.set_effort",
			"model.config.select_effort",
			"model.plan_config.set_model",
			"model.plan_config.select_model",
			"model.plan_config.set_effort",
			"model.plan_config.select_effort",
		},
	},
	{
		ID:       "menu.fast",
		Kind:     SpecKindCapability,
		Backends: []string{appruntime.BackendCodex},
		Commands: []CommandSpec{{ID: "fast", Names: []string{"/fast"}, HelpGroup: "model", HelpEntries: []HelpCommandSpec{{Command: "/fast", Summary: "切换当前线程的响应速度设置。"}, {Command: "/fast fast", Summary: "将当前线程的 service tier 设为 fast。"}, {Command: "/fast default", Summary: "将当前线程的 service tier 恢复为默认。"}, {Command: "/fast toggle", Summary: "切换当前线程的响应速度设置。"}, {Command: "/fast config", Summary: "打开当前线程的响应速度配置卡。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HideInHelp: true}}}},
		Nodes: []MenuNode{
			{Action: "menu.fast", Label: "响应速度", Parent: "menu.group.model"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.model", Action: "menu.fast", Label: "响应速度", Slash: "/fast config", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.fast",
			"service_tier.set",
		},
	},
	{
		ID:       "menu.debug",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "debug", Names: []string{"/debug"}, HelpGroup: "system", HelpEntries: []HelpCommandSpec{{Command: "/debug", Summary: "切换服务端 slog 日志级别（debug/info）。"}, {Command: "/debug on", Summary: "切换到 debug 级别。"}, {Command: "/debug off", Summary: "切换到 info 级别。"}, {Command: "/debug logs", Summary: "查看最近一段服务端 slog 日志。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.debug.logs", Label: "查看日志", Parent: "menu.group.system"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.system", Action: "menu.debug", Label: "日志级别", Slash: "/debug", Kind: MenuItemDirect},
			{GroupAction: "menu.group.system", Action: "menu.debug.logs", Label: "查看日志", Slash: "/debug logs", Kind: MenuItemDirect},
		},
		ActionNames: []ActionName{
			"menu.debug",
			"menu.debug.logs",
		},
	},
	{
		ID:       "menu.status",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "status", Names: []string{"/status"}, HelpGroup: "system", HelpEntries: []HelpCommandSpec{{Command: "/status", Summary: "查看当前会话、线程、工作区与模型状态。"}}, Backends: map[string]CommandBackendSpec{appruntime.BackendClaude: {HelpEntries: []HelpCommandSpec{{Command: "/status", Summary: "查看当前会话、工作区、Claude 模型与权限状态。"}}}}}},
		Nodes: []MenuNode{
			{Action: "menu.status", Label: "状态面板", Parent: "menu.group.system"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.system", Action: "menu.status", Label: "状态面板", Slash: "/status", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.status",
		},
	},
	{
		ID:       "menu.help",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "help", Names: []string{"/help"}, HelpEntries: []HelpCommandSpec{{Command: "/help", Summary: "查看所有本地命令与说明。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.help", Label: "命令帮助", Parent: "menu.group.system"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.system", Action: "menu.help", Label: "命令帮助", Slash: "/help", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.help",
		},
	},
	{
		ID:       "menu.codex_upgrade",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "codex", Names: []string{"/codex"}, HelpGroup: "system", HelpEntries: []HelpCommandSpec{{Command: "/codex", Summary: "查看本机 Codex CLI 的安装与升级状态。"}, {Command: "/codex check", Summary: "检查 Codex CLI 自升级命令。"}, {Command: "/codex upgrade", Summary: "通过 Codex CLI 自带升级命令更新，并做 runtime smoke test。"}, {Command: "/codex restart", Summary: "在空闲态原地重启 Codex runtime，适合刷新新安装的 Skill。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.codex_upgrade", Label: "Codex 管理", Parent: "menu.group.backend"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.backend", Action: "menu.codex_upgrade", Label: "Codex 管理", Slash: "/codex", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.codex_upgrade",
		},
	},
	{
		ID:       "menu.claude_upgrade",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "claude", Names: []string{"/claude"}, HelpGroup: "system", HelpEntries: []HelpCommandSpec{{Command: "/claude", Summary: "查看本机 Claude CLI 的安装与升级状态。"}, {Command: "/claude check", Summary: "检查 Claude CLI 自升级命令。"}, {Command: "/claude upgrade", Summary: "通过 Claude CLI 自带升级命令更新，并做 runtime smoke test。"}, {Command: "/claude restart", Summary: "在空闲态原地重启 Claude runtime。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.claude_upgrade", Label: "Claude 管理", Parent: "menu.group.backend"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.backend", Action: "menu.claude_upgrade", Label: "Claude 管理", Slash: "/claude", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.claude_upgrade",
		},
	},
	{
		ID:       "menu.upgrade",
		Kind:     SpecKindCapability,
		Commands: []CommandSpec{{ID: "upgrade", Names: []string{"/upgrade"}, HelpGroup: "system", HelpEntries: []HelpCommandSpec{{Command: "/upgrade", Summary: "检查最新版本并发起升级。"}, {Command: "/upgrade dev", Summary: "升级到 `dev-latest` 当前指向的开发版构建。"}, {Command: "/upgrade v0.3.0", Summary: "跳过最新版本检查，直接升级到指定版本。"}, {Command: "/upgrade local", Summary: "打开当前 workspace 的本地 Binary 选择器。"}, {Command: "/upgrade path ./dist/feidex-linux-amd64", Summary: "直接用当前 workspace 下的本地 Binary 发起升级确认。"}}}},
		Nodes: []MenuNode{
			{Action: "menu.upgrade", Label: "升级服务", Parent: "menu.group.system"},
		},
		MenuItems: []MenuItemSpec{
			{GroupAction: "menu.group.system", Action: "menu.upgrade", Label: "升级服务", Slash: "/upgrade", Kind: MenuItemSubmenu},
		},
		ActionNames: []ActionName{
			"menu.upgrade",
		},
	},
}

// All returns the registry entries.
func All() []Spec {
	return append([]Spec(nil), registry...)
}

// HelpGroupOrder returns the stable help section order.
func HelpGroupOrder() []string {
	return append([]string(nil), helpGroupOrder...)
}

// MenuNodes returns the derived menu node map.
func MenuNodes() map[string]MenuNode {
	nodes := make(map[string]MenuNode)
	for _, spec := range registry {
		for _, node := range spec.Nodes {
			nodes[strings.TrimSpace(node.Action)] = node
		}
		if spec.MenuGroup != nil {
			action := strings.TrimSpace(spec.MenuGroup.Action)
			if action == "" {
				continue
			}
			if _, exists := nodes[action]; exists {
				continue
			}
			nodes[action] = MenuNode{Action: action, Label: spec.MenuGroup.Label}
		}
	}
	return nodes
}

// MenuGroupSpecs returns the derived menu group specs.
func MenuGroupSpecs() []MenuGroupSpec {
	specs := make([]MenuGroupSpec, 0, 8)
	for _, spec := range registry {
		if spec.MenuGroup == nil {
			continue
		}
		specs = append(specs, *spec.MenuGroup)
	}
	return specs
}

// MenuItemSpecs returns the derived menu item specs.
func MenuItemSpecs() []MenuItemSpec {
	items := make([]MenuItemSpec, 0, 32)
	for _, spec := range registry {
		items = append(items, spec.MenuItems...)
	}
	return items
}

// FindSpecByID returns the registry entry by id.
func FindSpecByID(id string) (Spec, bool) {
	id = strings.TrimSpace(id)
	for _, spec := range registry {
		if spec.ID == id {
			return spec, true
		}
	}
	return Spec{}, false
}

// FindSpecByAction returns the feature or section that owns the action.
func FindSpecByAction(action string) (Spec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range registry {
		if spec.OwnsAction(action) {
			return spec, true
		}
	}
	return Spec{}, false
}

// FindCommand returns the feature and command metadata for the command name.
func FindCommand(name string) (Spec, CommandSpec, bool) {
	name = strings.TrimSpace(name)
	for _, spec := range registry {
		for _, command := range spec.Commands {
			for _, candidate := range command.Names {
				if strings.TrimSpace(candidate) == name {
					return spec, command, true
				}
			}
		}
	}
	return Spec{}, CommandSpec{}, false
}
