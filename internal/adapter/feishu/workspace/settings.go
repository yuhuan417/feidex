package workspace

import (
	"strings"

	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/feishu"
)

func (s *RenderService) RenderWorkspaceSettingsCard(key string, view workspaceapp.SettingsView) map[string]any {
	title, noun, field := "配置 Sandbox", "sandbox", "sandbox_mode"
	switch view.Setting {
	case workspaceapp.SettingPolicy:
		title, noun, field = "配置 Policy", "approval policy", "approval_policy"
	case workspaceapp.SettingMultiAgent:
		title, noun, field = "配置 Multi-Agent Mode", "multi-agent mode", "multi_agent_mode"
	case workspaceapp.SettingPermission:
		title, noun, field = "配置默认权限", "Claude 权限模式", "mode"
	}
	action := "workspace." + string(view.Setting) + ".set"
	value := func(setting string) map[string]any {
		return map[string]any{"action": action, "session_key": key, "workspace_id": view.WorkspaceID, field: setting}
	}
	body := "配置当前工作区默认 " + noun + "。\n\n当前工作区: `" + view.WorkspaceID + "`\n当前值: `" + view.CurrentValue + "`"
	buttons := make([]feishu.Button, 0, len(view.Options)+2)
	if view.RestoreDefault {
		buttons = append(buttons, feishu.Button{Text: "恢复默认", Type: "default", Value: value("")})
	}
	if view.Setting == workspaceapp.SettingPermission {
		override := "跟随全局"
		if view.CurrentValue != "" {
			override = "`" + view.CurrentValue + "`"
		}
		body = "配置当前工作区默认 Claude 权限模式。\n\n当前工作区: `" + view.WorkspaceID + "`\n生效值: `" + view.EffectiveValue + "`\n当前覆盖: " + override
		label, style := "跟随全局", "default"
		if view.Inherited {
			label, style = "当前 · 跟随全局", "primary"
		}
		buttons = append(buttons, feishu.Button{Text: label, Type: style, Value: value("")})
	}
	for _, option := range view.Options {
		label, style := option.Label, "default"
		if option.Value == view.CurrentValue {
			label, style = "当前 · "+label, "primary"
		}
		buttons = append(buttons, feishu.Button{Text: label, Type: style, Value: value(option.Value)})
	}
	buttons = append(buttons, feishu.Button{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.workspace", "session_key": key}})
	return feishu.SimpleStatusCard(title, "blue", s.FormatMenuBody(strings.TrimSuffix(action, ".set")+".menu", body), buttons)
}
