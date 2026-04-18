package app

import "strings"

type menuNode struct {
	Label  string
	Parent string
}

var menuNodes = map[string]menuNode{
	"menu.root":              {Label: "主菜单"},
	"menu.tools":             {Label: "常用工具", Parent: "menu.root"},
	"menu.group.model":       {Label: "模型配置", Parent: "menu.root"},
	"menu.group.system":      {Label: "系统运维", Parent: "menu.root"},
	"menu.thread":            {Label: "线程管理", Parent: "menu.root"},
	"menu.workspace":         {Label: "工作区管理", Parent: "menu.root"},
	"menu.review":            {Label: "代码审查", Parent: "menu.tools"},
	"menu.quiet":             {Label: "静默模式", Parent: "menu.tools"},
	"menu.usage":             {Label: "Token 消耗", Parent: "menu.tools"},
	"menu.history":           {Label: "历史记录", Parent: "menu.tools"},
	"menu.skills":            {Label: "技能列表", Parent: "menu.tools"},
	"history.detail":         {Label: "Turn 详情", Parent: "menu.history"},
	"workspace.new":          {Label: "新建工作区", Parent: "menu.workspace"},
	"workspace.clone":        {Label: "从仓库创建", Parent: "menu.workspace"},
	"workspace.sandbox.menu": {Label: "默认沙箱", Parent: "menu.workspace"},
	"workspace.policy.menu":  {Label: "默认策略", Parent: "menu.workspace"},
	"thread.sandbox.menu":    {Label: "线程沙箱", Parent: "menu.thread"},
	"thread.policy.menu":     {Label: "审批策略", Parent: "menu.thread"},
	"menu.model":             {Label: "模型配置", Parent: "menu.group.model"},
	"menu.fast":              {Label: "响应速度", Parent: "menu.group.model"},
	"menu.status":            {Label: "状态面板", Parent: "menu.group.system"},
	"menu.debug.logs":        {Label: "查看日志", Parent: "menu.group.system"},
	"menu.codex_upgrade":     {Label: "Codex 管理", Parent: "menu.group.system"},
	"menu.upgrade":           {Label: "升级服务", Parent: "menu.group.system"},
	"menu.help":              {Label: "命令帮助", Parent: "menu.group.system"},
}

func menuBreadcrumbLabels(action string) []string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "menu.root"
	}
	labels := []string{}
	for i := 0; action != "" && i < 16; i++ {
		node, ok := menuNodes[action]
		if !ok {
			break
		}
		labels = append(labels, node.Label)
		action = node.Parent
	}
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	return labels
}

func menuCardBody(action, body string) string {
	breadcrumbs := strings.Join(menuBreadcrumbLabels(action), " / ")
	body = strings.TrimSpace(body)
	if breadcrumbs == "" {
		return body
	}
	if body == "" {
		return "当前位置：" + breadcrumbs
	}
	return "当前位置：" + breadcrumbs + "\n\n" + body
}

func submenuLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "›"
	}
	return label + " ›"
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	return submenuLabel(commandLabel(label, slash))
}
