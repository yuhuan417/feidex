package workspace

import (
	"fmt"
	"strings"

	"feidex/internal/domain/backend"
	domain "feidex/internal/domain/workspace"
)

type Setting string

const (
	SettingSandbox    Setting = "sandbox"
	SettingPolicy     Setting = "policy"
	SettingMultiAgent Setting = "multiagent"
	SettingPermission Setting = "permission_mode"
)

type SettingsView struct {
	Setting        Setting
	WorkspaceID    string
	CurrentValue   string
	EffectiveValue string
	Options        []domain.SettingOption
	RestoreDefault bool
	Inherited      bool
}

func (s ViewService) Settings(key string, setting Setting) (SettingsView, error) {
	return s.Snapshot(key).Settings(setting)
}

func (v View) Settings(setting Setting) (SettingsView, error) {
	claude := v.Backend == backend.BackendClaude
	if (v.Backend != backend.BackendCodex && !claude) || claude != (setting == SettingPermission) {
		command := string(setting)
		if setting == SettingPermission {
			command = "permissions"
		}
		return SettingsView{}, fmt.Errorf("当前 backend 不支持 /workspace %s", command)
	}
	ws := v.SettingsWorkspace
	if ws == nil {
		return SettingsView{}, fmt.Errorf("current workspace not found")
	}
	view := SettingsView{Setting: setting, WorkspaceID: ws.ID}
	switch setting {
	case SettingSandbox:
		view.CurrentValue, view.Options, view.RestoreDefault = ws.SandboxMode, domain.SandboxOptions(), true
	case SettingPolicy:
		view.CurrentValue, view.Options, view.RestoreDefault = ws.ApprovalPolicy, domain.ApprovalPolicyOptions(), true
	case SettingMultiAgent:
		view.CurrentValue, view.Options = ws.MultiAgentMode, domain.MultiAgentModeOptions()
	case SettingPermission:
		view.CurrentValue = strings.TrimSpace(ws.ClaudePermissionMode)
		view.Inherited = view.CurrentValue == ""
		view.EffectiveValue = view.CurrentValue
		if view.EffectiveValue == "" {
			view.EffectiveValue = strings.TrimSpace(v.ClaudePermissionMode)
		}
		if view.EffectiveValue == "" {
			view.EffectiveValue = "default"
		}
		view.Options = []domain.SettingOption{{Value: "default", Label: "default"}, {Value: "acceptEdits", Label: "acceptEdits"}}
		if v.ClaudeBypassEnabled {
			view.Options = append(view.Options, domain.SettingOption{Value: "bypassPermissions", Label: "bypassPermissions"})
		}
	default:
		return SettingsView{}, fmt.Errorf("unknown workspace setting %q", setting)
	}
	return view, nil
}
