package workspace

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
	"fmt"
	"strings"
)

type SettingsRevision struct {
	Workspace   *domain.Workspace
	Session     *conversation.Session
	Profile     *routing.BotProfile
	AllowBypass bool
}
type SettingsRepository interface {
	UpdateSettings(string, string, func(*SettingsRevision) error) error
}
type SettingsService struct {
	Repository SettingsRepository
	Frontend   string
}

func (s SettingsService) Set(key, id string, setting routing.Setting, raw string) error {
	if strings.TrimSpace(s.Frontend) == "" {
		s.Frontend = "default"
	}
	return s.Repository.UpdateSettings(key, id, func(revision *SettingsRevision) error {
		value := strings.TrimSpace(raw)
		if setting == routing.Permissions {
			switch value {
			case "inherit", "follow", "clear", "unset":
				value = ""
			case "", "default", "acceptEdits":
			case "bypassPermissions":
				if !revision.AllowBypass {
					return fmt.Errorf("当前未启用 claude.dangerously_skip_permissions")
				}
			default:
				return fmt.Errorf("不支持的 Claude 权限模式")
			}
		} else {
			var options []domain.SettingOption
			switch setting {
			case routing.Sandbox:
				options = domain.SandboxOptions()
			case routing.ApprovalPolicy:
				options = domain.ApprovalPolicyOptions()
			case routing.MultiAgent:
				options = domain.MultiAgentModeOptions()
			default:
				return fmt.Errorf("unsupported workspace setting %q", setting)
			}
			valid := value == "" && setting != routing.MultiAgent
			for _, opt := range options {
				if opt.Value == value {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("不支持的 %s", setting)
			}
		}
		ws := revision.Workspace
		if ws == nil {
			return fmt.Errorf("workspace %q not found", id)
		}
		switch setting {
		case routing.Sandbox:
			ws.SandboxMode = value
		case routing.ApprovalPolicy:
			ws.ApprovalPolicy = value
		case routing.MultiAgent:
			ws.MultiAgentMode = value
		case routing.Permissions:
			ws.ClaudePermissionMode = value
		}
		if revision.Session != nil && revision.Session.ChatType == "p2p" {
			if revision.Profile == nil {
				revision.Profile = &routing.BotProfile{ID: "bot-profile-" + routing.ProfileID(s.Frontend), FrontendID: s.Frontend}
			}
			p := revision.Profile
			switch setting {
			case routing.Sandbox:
				p.SandboxMode = value
			case routing.ApprovalPolicy:
				p.ApprovalPolicy = value
			case routing.MultiAgent:
				p.MultiAgentMode = value
			case routing.Permissions:
				p.ClaudePermissionMode = value
			}
		}
		return nil
	})
}
