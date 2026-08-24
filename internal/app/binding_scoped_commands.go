package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const groupBindingWorkspaceUsage = "/workspace | /workspace list | /workspace choose | /workspace use ID | /workspace new ID CWD | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace sandbox MODE|default | /workspace policy POLICY|default | /workspace multiagent MODE|default | /workspace permissions MODE|default"

func isGroupMessage(msg *feishu.InboundMessage) bool {
	return msg != nil && strings.TrimSpace(msg.ChatType) == "group" && strings.TrimSpace(msg.ChatID) != ""
}

func groupBindingScopeActive(a *App, msg *feishu.InboundMessage) bool {
	return isGroupMessage(msg) && agentBindingForChat(a, msg.ChatType, msg.ChatID) != nil
}

func isGroupSessionKey(sessionKey string) bool {
	chatType, chatID, _, _ := parseSessionKeyMeta(sessionKey)
	return chatType == "group" && strings.TrimSpace(chatID) != ""
}

func sessionKeyChat(sessionKey string) (chatType, chatID string) {
	chatType, chatID, _, _ = parseSessionKeyMeta(sessionKey)
	return chatType, chatID
}

func bindingForSessionKey(a *App, sessionKey string) *state.AgentBinding {
	chatType, chatID := sessionKeyChat(sessionKey)
	if chatType != "group" || strings.TrimSpace(chatID) == "" {
		return nil
	}
	return agentBindingForChat(a, chatType, chatID)
}

func groupBindingSessionScopeActive(a *App, sessionKey string) bool {
	return bindingForSessionKey(a, sessionKey) != nil
}

func groupBindingBackButton(sessionKey string) feishu.Button {
	return feishu.Button{Text: "返回当前 Bot", Type: "default", Value: map[string]any{"action": "menu.current_bot", "session_key": sessionKey}}
}

func (s bindingService) commandWorkspace(msg *feishu.InboundMessage, args []string) error {
	if !isGroupMessage(msg) {
		return commandWorkspace(s.app, msg, args)
	}
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		card := s.renderBindingStatusCard(makeSessionKey(s.app, msg), binding)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list", "choose":
		card := s.renderBindingWorkspaceChooseCard(makeSessionKey(s.app, msg), binding)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	case "use":
		return s.commandBind(msg, append([]string{"use"}, args[1:]...))
	case "new":
		if len(args) < 3 {
			return fmt.Errorf("usage: /workspace new WORKSPACE_ID CWD")
		}
		return s.commandBind(msg, append([]string{"new"}, args[1:]...))
	case "clone":
		if len(args) < 2 {
			return fmt.Errorf("usage: /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]")
		}
		return s.commandBind(msg, append([]string{"clone"}, args[1:]...))
	case "sandbox", "policy", "multiagent", "permissions", "permission":
		if len(args) == 1 {
			card := s.renderBindingStatusCard(makeSessionKey(s.app, msg), binding)
			_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
			return err
		}
		return s.commandBind(msg, append([]string{strings.ToLower(strings.TrimSpace(args[0]))}, args[1:]...))
	case "delete":
		return fmt.Errorf("群聊中的 workspace 是当前 bot 的 binding 配置；删除本机 workspace 请私聊该 bot 使用 /workspace delete")
	default:
		return fmt.Errorf("usage: %s", groupBindingWorkspaceUsage)
	}
}

func (s bindingService) commandModel(msg *feishu.InboundMessage, args []string) error {
	if !isGroupMessage(msg) {
		return newBackendConfigurationService(s.app).handleBackendModelCommand(msg, args)
	}
	if len(args) == 0 {
		return s.commandBind(msg, []string{"status"})
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "set":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model set MODEL|default")
		}
		return s.commandBind(msg, []string{"model", args[1]})
	case "effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model effort EFFORT|default")
		}
		return s.commandBind(msg, []string{"effort", args[1]})
	case "plan":
		return fmt.Errorf("/model plan 是当前 bot 的 frontend 默认配置；请私聊该 bot 配置，群聊中只修改 binding 的 /model set 和 /model effort")
	case "option":
		return fmt.Errorf("/model option 是当前 bot 的模型候选列表配置；请私聊该 bot 使用")
	default:
		return fmt.Errorf("usage: /model | /model set MODEL|default | /model effort EFFORT|default")
	}
}

func (s bindingService) commandEffort(msg *feishu.InboundMessage, args []string) error {
	if !isGroupMessage(msg) {
		return newModelConfigService(s.app).commandEffort(msg, args)
	}
	switch len(args) {
	case 0:
		return s.commandBind(msg, []string{"status"})
	case 1:
		return s.commandBind(msg, []string{"effort", args[0]})
	default:
		return fmt.Errorf("usage: /effort | /effort EFFORT|default")
	}
}

func (s bindingService) commandFast(msg *feishu.InboundMessage, args []string) error {
	if !isGroupMessage(msg) {
		return commandFast(s.app, msg, args)
	}
	if len(args) == 0 {
		args = []string{"toggle"}
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast off | /fast toggle | /fast config")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "config":
		return s.commandBind(msg, []string{"status"})
	case "fast", "default", "off":
		return s.commandBind(msg, []string{"fast", args[0]})
	case "toggle":
		binding, err := s.ensureBindingForMessage(msg)
		if err != nil {
			return err
		}
		next := toggleServiceTier(binding.ServiceTierOverride)
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ServiceTierOverride = next })
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新 binding fast: "+renderOptionalBacktick(updated.ServiceTierOverride))
	default:
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast off | /fast toggle | /fast config")
	}
}

func (s bindingService) completeBindingWorkspaceChoose(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := commandMessageFromAction(s.app, action, sessionKey, "/workspace choose")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择当前 Bot 的 workspace"},
		Card:  rawCard(s.renderBindingWorkspaceChooseCard(sessionKey, binding)),
	}, nil
}

func (s bindingService) completeBindingModelSet(action *feishu.CardAction, sessionKey, modelID string) (*callback.CardActionTriggerResponse, error) {
	modelID = clearableArg(modelID)
	msg := commandMessageFromAction(s.app, action, sessionKey, "/bind model")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ModelOverride = modelID })
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 binding model"},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}

func (s bindingService) completeBindingEffortSet(action *feishu.CardAction, sessionKey, effort string) (*callback.CardActionTriggerResponse, error) {
	effort = clearableArg(effort)
	msg := commandMessageFromAction(s.app, action, sessionKey, "/bind effort")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ReasoningEffortOverride = effort })
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 binding effort"},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}

func (s bindingService) completeBindingServiceTierSet(action *feishu.CardAction, sessionKey, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	serviceTier = clearableArg(serviceTier)
	if serviceTier != "" {
		serviceTier = normalizeServiceTier(serviceTier)
		if serviceTier == "" {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "unsupported service tier"}}, nil
		}
	}
	msg := commandMessageFromAction(s.app, action, sessionKey, "/bind fast")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ServiceTierOverride = serviceTier })
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 binding fast"},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}

func (s bindingService) completeBindingSimpleOverride(action *feishu.CardAction, sessionKey, fieldName, value string) (*callback.CardActionTriggerResponse, error) {
	msg := commandMessageFromAction(s.app, action, sessionKey, "/bind "+fieldName)
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	value = clearableArg(value)
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
		switch fieldName {
		case "sandbox":
			current.SandboxModeOverride = value
		case "policy":
			current.ApprovalPolicyOverride = value
		case "multiagent":
			current.MultiAgentModeOverride = value
		case "permissions":
			current.ClaudePermissionMode = value
		}
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 binding " + fieldName},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}
