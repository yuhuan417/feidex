package app

import (
	appservicetiercmd "feidex/internal/adapter/feishu/servicetier"
	"feidex/internal/domain/conversation"

	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func commandWorkspaceProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if err := commandWorkspace(a, msg, args); err != nil {
		return err
	}
	// Workspace management already handles creation, clone and selection. The
	// p2p profile mirrors the resulting selection after the command succeeds.
	if msg == nil || len(args) == 0 || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return nil
	}
	if len(args) >= 2 {
		name := strings.ToLower(strings.TrimSpace(args[0]))
		value := clearableArg(args[1])
		var field func(*state.BotProfile)
		switch name {
		case "sandbox":
			field = func(profile *state.BotProfile) { profile.SandboxMode = value }
		case "policy":
			field = func(profile *state.BotProfile) { profile.ApprovalPolicy = value }
		case "multiagent":
			field = func(profile *state.BotProfile) { profile.MultiAgentMode = value }
		case "permissions", "permission":
			field = func(profile *state.BotProfile) { profile.ClaudePermissionMode = value }
		}
		if field != nil {
			_, err := newRoutingConfiguration(a).UpdateProfile(field)
			return err
		}
	}
	_, sess, ws := currentWorkspaceForMessage(a, msg)
	workspaceID := ""
	if ws != nil {
		workspaceID = ws.ID
	} else if sess != nil {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	if workspaceID == "" {
		return nil
	}
	_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.WorkspaceID = workspaceID })
	return err
}

func commandModelProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
	}
	if len(args) == 0 {
		return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
	}
	if len(args) == 3 && strings.EqualFold(strings.TrimSpace(args[1]), "set") {
		role := strings.ToLower(strings.TrimSpace(args[0]))
		value := clearableArg(args[2])
		if role == "plan" || role == "review" || role == "subagent" || role == "small" {
			if err := ensureSessionModelConfigWritable(a, makeSessionKey(a, msg)); err != nil {
				return err
			}
			backend := configuredBackend(a)
			if sess := a.State().Session(makeSessionKey(a, msg)); sess != nil {
				if _, err := a.State().UpdateSession(sess.Key, func(current *conversation.Session) {
					switch role {
					case "plan":
						current.PlanModelOverride = value
					case "review":
						current.ReviewModelOverride = value
					case "subagent":
						current.SubagentModelOverride = value
					case "small":
						current.SmallModelOverride = value
					}
				}); err != nil {
					return err
				}
				return replyTextEffect(a, msg, "已更新当前 session 的 "+role+" model")
			}
			_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) {
				if backend == config.RuntimeBackendClaude {
					switch role {
					case "small":
						profile.ClaudeSmallModel = value
					case "subagent":
						profile.ClaudeSubagentModel = value
					}
				} else {
					switch role {
					case "plan":
						profile.PlanModel = value
					case "review":
						profile.ReviewModel = value
					case "subagent":
						profile.SubagentModel = value
					}
				}
			})
			if err != nil {
				return err
			}
			return replyTextEffect(a, msg, "已更新当前 Bot 的 "+role+" model")
		}
	}
	if len(args) == 3 && strings.EqualFold(strings.TrimSpace(args[1]), "effort") && strings.EqualFold(strings.TrimSpace(args[0]), "subagent") && configuredBackend(a) == config.RuntimeBackendCodex {
		if err := ensureSessionModelConfigWritable(a, makeSessionKey(a, msg)); err != nil {
			return err
		}
		value := clearableArg(args[2])
		if sess := a.State().Session(makeSessionKey(a, msg)); sess != nil {
			_, err := a.State().UpdateSession(sess.Key, func(current *conversation.Session) { current.SubagentReasoningEffortOverride = value })
			if err != nil {
				return err
			}
			return replyTextEffect(a, msg, "已更新当前 session 的 subagent reasoning effort")
		}
		_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.SubagentReasoningEffort = value })
		if err != nil {
			return err
		}
		return replyTextEffect(a, msg, "已更新当前 Bot 的 subagent reasoning effort")
	}
	if len(args) == 3 && strings.EqualFold(strings.TrimSpace(args[1]), "effort") && strings.EqualFold(strings.TrimSpace(args[0]), "plan") && configuredBackend(a) == config.RuntimeBackendCodex {
		if err := ensureSessionModelConfigWritable(a, makeSessionKey(a, msg)); err != nil {
			return err
		}
		value := clearableArg(args[2])
		if sess := a.State().Session(makeSessionKey(a, msg)); sess != nil {
			_, err := a.State().UpdateSession(sess.Key, func(current *conversation.Session) { current.PlanReasoningEffortOverride = value })
			if err != nil {
				return err
			}
			return replyTextEffect(a, msg, "已更新当前 session 的 Plan reasoning effort")
		}
		_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.PlanReasoningEffort = value })
		if err != nil {
			return err
		}
		return replyTextEffect(a, msg, "已更新当前 Bot 的 Plan reasoning effort")
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "set") && len(args) == 2 {
		if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, args); err != nil {
			return err
		}
		backend := configuredBackend(a)
		value := clearableArg(args[1])
		_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) {
			if backend == config.RuntimeBackendClaude {
				profile.ClaudeModel = value
			} else {
				profile.Model = value
			}
		})
		if err != nil {
			return err
		}
		return nil
	}
	// Keep plan/option subcommands on their existing configuration handlers;
	// those are backend catalog settings rather than Conversation runtime state.
	return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
}

func commandEffortProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return newModelConfigService(a).commandEffort(msg, args)
	}
	if len(args) == 0 {
		return newModelConfigService(a).commandEffort(msg, args)
	}
	if len(args) == 1 {
		if err := newModelConfigService(a).commandEffort(msg, args); err != nil {
			return err
		}
		value := clearableArg(args[0])
		_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.ReasoningEffort = value })
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("usage: /effort | /effort EFFORT|default")
}

func commandFastProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return commandFast(a, msg, args)
	}
	if len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		return commandFast(a, msg, args)
	}
	if len(args) == 0 || (len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "toggle")) {
		profile, err := newRoutingConfiguration(a).EnsureProfile()
		if err != nil {
			return err
		}
		next := appservicetiercmd.ToggleServiceTier(profile.ServiceTier)
		_, err = newRoutingConfiguration(a).UpdateProfile(func(p *state.BotProfile) { p.ServiceTier = next })
		if err != nil {
			return err
		}
		return replyTextEffect(a, msg, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(next))
	}
	if len(args) == 1 {
		value := strings.ToLower(strings.TrimSpace(args[0]))
		if value == "off" || value == "default" {
			value = ""
		} else {
			value = appservicetiercmd.NormalizeServiceTier(value)
			if value == "" {
				return fmt.Errorf("unsupported service tier %q", args[0])
			}
		}
		_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.ServiceTier = value })
		if err != nil {
			return err
		}
		return replyTextEffect(a, msg, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(value))
	}
	return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast off | /fast toggle")
}

func effectiveBotProfile(a *App) *state.BotProfile {
	if a == nil || a.State() == nil {
		return nil
	}
	return a.State().BotProfile()
}

func completeBotProfileModelSet(a *App, action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	var resp *callback.CardActionTriggerResponse
	var err error
	if configuredBackend(a) == config.RuntimeBackendClaude {
		resp, err = newBackendConfigurationService(a).completeGlobalModelSet(action, modelID)
	} else {
		resp, err = newBackendConfigurationService(a).completeGlobalModelSet(action, modelID)
	}
	if err != nil || resp == nil || (resp.Toast != nil && strings.EqualFold(resp.Toast.Type, "error")) {
		return resp, err
	}
	value := clearableArg(modelID)
	_, err = newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) {
		if configuredBackend(a) == config.RuntimeBackendClaude {
			profile.ClaudeModel = value
		} else {
			profile.Model = value
		}
	})
	return resp, err
}

func completeBotProfileEffortSet(a *App, action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	resp, err := newBackendConfigurationService(a).completeGlobalReasoningEffortSet(action, effort)
	if err != nil || resp == nil || (resp.Toast != nil && strings.EqualFold(resp.Toast.Type, "error")) {
		return resp, err
	}
	_, err = newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.ReasoningEffort = clearableArg(effort) })
	return resp, err
}

func completeBotProfileAuxiliaryModelSet(a *App, action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	if err := ensureSessionModelConfigWritable(a, sessionKey); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	value = clearableArg(value)
	backend := configuredBackend(a)
	if sess := a.State().Session(sessionKey); sess != nil {
		if _, err := a.State().UpdateSession(sess.Key, func(current *conversation.Session) {
			switch role {
			case "plan":
				current.PlanModelOverride = value
			case "plan_effort":
				current.PlanReasoningEffortOverride = value
			case "review":
				current.ReviewModelOverride = value
			case "subagent":
				current.SubagentModelOverride = value
			case "subagent_effort":
				current.SubagentReasoningEffortOverride = value
			case "small":
				current.SmallModelOverride = value
			}
		}); err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存当前 session 的辅助模型配置；待对应会话边界生效"}}, nil
	}
	_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) {
		if backend == config.RuntimeBackendClaude {
			switch role {
			case "small":
				profile.ClaudeSmallModel = value
			case "subagent":
				profile.ClaudeSubagentModel = value
			}
			return
		}
		switch role {
		case "plan":
			profile.PlanModel = value
		case "plan_effort":
			profile.PlanReasoningEffort = value
		case "review":
			profile.ReviewModel = value
		case "subagent":
			profile.SubagentModel = value
		case "subagent_effort":
			profile.SubagentReasoningEffort = value
		}
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存当前 Bot 的辅助模型配置；待对应会话边界生效"}}, nil
}

func completeBotProfileServiceTierSet(a *App, action *feishu.CardAction, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	value := appservicetiercmd.NormalizeServiceTier(serviceTier)
	if strings.EqualFold(strings.TrimSpace(serviceTier), "default") || strings.EqualFold(strings.TrimSpace(serviceTier), "off") {
		value = ""
	}
	if strings.TrimSpace(serviceTier) != "" && value == "" && !strings.EqualFold(strings.TrimSpace(serviceTier), "default") && !strings.EqualFold(strings.TrimSpace(serviceTier), "off") {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "unsupported service tier"}}, nil
	}
	_, err := newRoutingConfiguration(a).UpdateProfile(func(profile *state.BotProfile) { profile.ServiceTier = value })
	if err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前 Bot 默认响应速度"}, Card: rawCard(renderServiceTierMenuCard(a, actionSessionKey(action)))}, nil
}
