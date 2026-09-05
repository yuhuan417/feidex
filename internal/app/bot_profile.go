package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// botProfileForApp returns the frontend-scoped profile, creating an empty
// profile lazily so every p2p configuration command has one stable target.
func botProfileForApp(a *App) (*state.BotProfile, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	if profile := a.State().BotProfile(); profile != nil {
		return profile, nil
	}
	profile := &state.BotProfile{
		ID:         "bot-profile-" + sanitizeProfileID(a.FrontendID()),
		FrontendID: a.FrontendID(),
	}
	if err := a.State().SaveBotProfile(profile); err != nil {
		return nil, err
	}
	return a.State().BotProfile(), nil
}

func sanitizeProfileID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func updateBotProfile(a *App, mutate func(*state.BotProfile)) (*state.BotProfile, error) {
	profile, err := botProfileForApp(a)
	if err != nil {
		return nil, err
	}
	if mutate != nil {
		mutate(profile)
	}
	if err := a.State().SaveBotProfile(profile); err != nil {
		return nil, err
	}
	return a.State().BotProfile(), nil
}

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
			_, err := updateBotProfile(a, field)
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
	_, err := updateBotProfile(a, func(profile *state.BotProfile) { profile.WorkspaceID = workspaceID })
	return err
}

func commandModelProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
	}
	if len(args) == 0 {
		return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "set") && len(args) == 2 {
		if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, args); err != nil {
			return err
		}
		backend := configuredBackend(a)
		value := clearableArg(args[1])
		_, err := updateBotProfile(a, func(profile *state.BotProfile) {
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
		_, err := updateBotProfile(a, func(profile *state.BotProfile) { profile.ReasoningEffort = value })
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
		profile, err := botProfileForApp(a)
		if err != nil {
			return err
		}
		next := toggleServiceTier(profile.ServiceTier)
		_, err = updateBotProfile(a, func(p *state.BotProfile) { p.ServiceTier = next })
		if err != nil {
			return err
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(next), replyInThreadEnabled(a, msg.ChatType))
	}
	if len(args) == 1 {
		value := strings.ToLower(strings.TrimSpace(args[0]))
		if value == "off" || value == "default" {
			value = ""
		} else {
			value = normalizeServiceTier(value)
			if value == "" {
				return fmt.Errorf("unsupported service tier %q", args[0])
			}
		}
		_, err := updateBotProfile(a, func(profile *state.BotProfile) { profile.ServiceTier = value })
		if err != nil {
			return err
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(value), replyInThreadEnabled(a, msg.ChatType))
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
	_, err = updateBotProfile(a, func(profile *state.BotProfile) {
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
	_, err = updateBotProfile(a, func(profile *state.BotProfile) { profile.ReasoningEffort = clearableArg(effort) })
	return resp, err
}

func completeBotProfileServiceTierSet(a *App, action *feishu.CardAction, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	value := normalizeServiceTier(serviceTier)
	if strings.EqualFold(strings.TrimSpace(serviceTier), "default") || strings.EqualFold(strings.TrimSpace(serviceTier), "off") {
		value = ""
	}
	if strings.TrimSpace(serviceTier) != "" && value == "" && !strings.EqualFold(strings.TrimSpace(serviceTier), "default") && !strings.EqualFold(strings.TrimSpace(serviceTier), "off") {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "unsupported service tier"}}, nil
	}
	_, err := updateBotProfile(a, func(profile *state.BotProfile) { profile.ServiceTier = value })
	if err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前 Bot 默认响应速度"}, Card: rawCard(renderServiceTierMenuCard(a, actionSessionKey(action)))}, nil
}
