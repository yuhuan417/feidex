package app

import "strings"

type menuNode struct {
	Label  string
	Parent string
}

var menuNodes = map[string]menuNode{
	"menu.root":              {Label: "命令菜单"},
	"menu.group.session":     {Label: "会话行为", Parent: "menu.root"},
	"menu.group.context":     {Label: "会话管理", Parent: "menu.root"},
	"menu.group.model":       {Label: "模型能力", Parent: "menu.root"},
	"menu.group.system":      {Label: "服务管理", Parent: "menu.root"},
	"menu.quiet":             {Label: "Quiet 模式", Parent: "menu.group.session"},
	"menu.workspace":         {Label: "工作区管理", Parent: "menu.group.context"},
	"workspace.new":          {Label: "新建工作区", Parent: "menu.workspace"},
	"workspace.sandbox.menu": {Label: "配置 Sandbox", Parent: "menu.workspace"},
	"workspace.policy.menu":  {Label: "配置 Policy", Parent: "menu.workspace"},
	"menu.threads":           {Label: "线程管理", Parent: "menu.group.context"},
	"thread.sandbox.menu":    {Label: "配置 Thread Sandbox", Parent: "menu.threads"},
	"thread.policy.menu":     {Label: "配置 Thread Policy", Parent: "menu.threads"},
	"menu.model":             {Label: "模型配置", Parent: "menu.group.model"},
	"menu.reasoning":         {Label: "推理强度", Parent: "menu.group.model"},
	"menu.fast":              {Label: "响应速度", Parent: "menu.group.model"},
	"menu.status":            {Label: "状态面板", Parent: "menu.group.system"},
	"menu.upgrade":           {Label: "升级服务", Parent: "menu.group.system"},
	"menu.help":              {Label: "帮助说明", Parent: "menu.group.system"},
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
