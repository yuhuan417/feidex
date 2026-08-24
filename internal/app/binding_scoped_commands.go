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
	return isGroupMessage(msg)
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
	return isGroupSessionKey(sessionKey)
}

func groupBindingBackButton(sessionKey string) feishu.Button {
	return feishu.Button{Text: "返回工作区", Type: "default", Value: map[string]any{"action": "menu.workspace", "session_key": sessionKey}}
}

func (s bindingService) commandWorkspace(msg *feishu.InboundMessage, args []string) error {
	if !isGroupMessage(msg) {
		return commandWorkspace(s.app, msg, args)
	}
	if _, err := s.ensureBindingForMessage(msg); err != nil {
		return err
	}
	sessionKey := makeSessionKey(s.app, msg)
	if len(args) == 0 {
		card := newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		card := newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	case "choose":
		card := newWorkspaceRenderServiceInner(s.app).RenderWorkspaceChooseCard(sessionKey)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	case "use":
		return s.commandCurrentBotGroupConfig(msg, append([]string{"use"}, args[1:]...))
	case "new":
		if len(args) == 1 {
			return newWorkspaceManagementServiceInner(s.app).BeginWorkspaceNew(msg)
		}
		if len(args) < 3 {
			return fmt.Errorf("usage: /workspace new WORKSPACE_ID CWD")
		}
		return s.commandCurrentBotGroupConfig(msg, append([]string{"new"}, args[1:]...))
	case "clone":
		if len(args) == 1 {
			action := commandActionFromMessage(msg, map[string]any{"session_key": sessionKey})
			resp, err := newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceClone(action, sessionKey)
			if err != nil {
				return err
			}
			return replyCommandActionResponse(s.app, msg, resp)
		}
		if len(args) < 2 {
			return fmt.Errorf("usage: /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]")
		}
		return s.commandCurrentBotGroupConfig(msg, append([]string{"clone"}, args[1:]...))
	case "sandbox", "policy", "multiagent", "permissions", "permission":
		if len(args) == 1 {
			fieldName := strings.ToLower(strings.TrimSpace(args[0]))
			if fieldName == "permission" {
				fieldName = "permissions"
			}
			card, err := s.renderBindingWorkspaceSettingCard(sessionKey, bindingForSessionKey(s.app, sessionKey), fieldName)
			if err != nil {
				return err
			}
			_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
			return err
		}
		return s.commandCurrentBotGroupConfig(msg, append([]string{strings.ToLower(strings.TrimSpace(args[0]))}, args[1:]...))
	case "delete":
		return fmt.Errorf("群聊中的 workspace 是当前 Bot 在本群的工作区配置；删除本机 workspace 请私聊该 Bot 使用 /workspace delete")
	default:
		return fmt.Errorf("usage: %s", groupBindingWorkspaceUsage)
	}
}

func (s bindingService) commandModel(msg *feishu.InboundMessage, args []string) error {
	if !isGroupMessage(msg) {
		return newBackendConfigurationService(s.app).handleBackendModelCommand(msg, args)
	}
	if len(args) == 0 {
		return s.commandCurrentBotGroupConfig(msg, []string{"status"})
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "set":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model set MODEL|default")
		}
		return s.commandCurrentBotGroupConfig(msg, []string{"model", args[1]})
	case "effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model effort EFFORT|default")
		}
		return s.commandCurrentBotGroupConfig(msg, []string{"effort", args[1]})
	case "plan":
		return fmt.Errorf("/model plan 是当前 Bot 的默认配置；请私聊该 Bot 配置，群聊中只修改当前群内的 /model set 和 /model effort")
	case "option":
		return fmt.Errorf("/model option 是当前 Bot 的模型候选列表配置；请私聊该 Bot 使用")
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
		return s.commandCurrentBotGroupConfig(msg, []string{"status"})
	case 1:
		return s.commandCurrentBotGroupConfig(msg, []string{"effort", args[0]})
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
		return s.commandCurrentBotGroupConfig(msg, []string{"status"})
	case "fast", "default", "off":
		return s.commandCurrentBotGroupConfig(msg, []string{"fast", args[0]})
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
		return s.replyBindingUpdated(msg, "已更新当前群内响应速度: "+renderOptionalBacktick(updated.ServiceTierOverride))
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
	msg := commandMessageFromAction(s.app, action, sessionKey, "/model")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ModelOverride = modelID })
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新当前群内模型"},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}

func (s bindingService) completeBindingEffortSet(action *feishu.CardAction, sessionKey, effort string) (*callback.CardActionTriggerResponse, error) {
	effort = clearableArg(effort)
	msg := commandMessageFromAction(s.app, action, sessionKey, "/model effort")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ReasoningEffortOverride = effort })
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新当前群内推理强度"},
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
	msg := commandMessageFromAction(s.app, action, sessionKey, "/fast")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { current.ServiceTierOverride = serviceTier })
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新当前群内响应速度"},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}

func (s bindingService) completeBindingSimpleOverride(action *feishu.CardAction, sessionKey, fieldName, value string) (*callback.CardActionTriggerResponse, error) {
	msg := commandMessageFromAction(s.app, action, sessionKey, "/workspace "+fieldName)
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
	card, err := s.renderBindingWorkspaceSettingCard(sessionKey, updated, fieldName)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新当前群内 " + fieldName},
		Card:  rawCard(card),
	}, nil
}
