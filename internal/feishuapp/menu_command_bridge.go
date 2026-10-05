package feishuapp

import (
	identity "feidex/internal/domain/identity"
	"feidex/internal/textutil"
	"strings"

	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func parseSessionKeyMeta(sessionKey string) (chatType, chatID, rootMessageID, userID string) {
	_, chatType, chatID, rootMessageID, userID = identity.ParseSessionKey(sessionKey)
	return chatType, chatID, rootMessageID, userID
}

func commandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if actionValue == nil {
		actionValue = map[string]any{}
	}
	if msg == nil {
		return &feishu.CardAction{ActionValue: actionValue}
	}
	return &feishu.CardAction{
		ActionValue: actionValue,
		UserID:      strings.TrimSpace(msg.UserID),
		ChatID:      strings.TrimSpace(msg.ChatID),
		MessageID:   strings.TrimSpace(msg.MessageID),
	}
}

func commandMessageFromAction(scope bindingSessionScope, action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
	msg := &feishu.InboundMessage{
		SessionKey: strings.TrimSpace(sessionKey),
		MessageID:  strings.TrimSpace(action.MessageID),
		ChatID:     strings.TrimSpace(action.ChatID),
		UserID:     strings.TrimSpace(action.UserID),
		Text:       strings.TrimSpace(rawCommand),
	}
	chatType, chatID, rootMessageID, sessionUserID := parseSessionKeyMeta(sessionKey)
	if msg.ChatType == "" {
		msg.ChatType = chatType
	}
	if msg.ChatID == "" {
		msg.ChatID = chatID
	}
	if msg.RootMessageID == "" {
		msg.RootMessageID = rootMessageID
	}
	if msg.UserID == "" {
		msg.UserID = sessionUserID
	}
	if sess := scope.state.Session(sessionKey); sess != nil {
		msg.ChatID = textutil.FirstNonEmpty(msg.ChatID, strings.TrimSpace(sess.ChatID))
		msg.ChatType = textutil.FirstNonEmpty(msg.ChatType, strings.TrimSpace(sess.ChatType))
		msg.UserID = textutil.FirstNonEmpty(msg.UserID, strings.TrimSpace(sess.OwnerUserID))
	}
	if msg.ChatType == "" || msg.ChatID == "" {
		inferredChatType, inferredChatID := scope.chat(sessionKey)
		msg.ChatType = textutil.FirstNonEmpty(msg.ChatType, inferredChatType)
		msg.ChatID = textutil.FirstNonEmpty(msg.ChatID, inferredChatID)
	}
	if msg.ChatType == "group" && strings.TrimSpace(msg.RootMessageID) == "" {
		msg.RootMessageID = textutil.FirstNonEmpty(rootMessageID, msg.MessageID)
	}
	return msg
}

type MenuCommandInputs struct {
	BindingScope   BindingScope
	Capture        FeishuClient
	HandleCommand  func(*feishu.InboundMessage, string) error
	RenderFallback func(string, string) (map[string]any, bool)
}

type MenuCommandService struct {
	scope          bindingSessionScope
	capture        appfeishuwrap.CommandCaptureFeishuClient
	handleCommand  func(*feishu.InboundMessage, string) error
	renderFallback func(string, string) (map[string]any, bool)
}

func NewMenuCommandService(inputs MenuCommandInputs) MenuCommandService {
	var capture appfeishuwrap.CommandCaptureFeishuClient
	if candidate, ok := inputs.Capture.(appfeishuwrap.CommandCaptureFeishuClient); ok {
		capture = candidate
	}
	return MenuCommandService{
		scope:          inputs.BindingScope.scope,
		capture:        capture,
		handleCommand:  inputs.HandleCommand,
		renderFallback: inputs.RenderFallback,
	}
}

func (s MenuCommandService) run(action *feishu.CardAction, sessionKey, rawCommand string) (string, map[string]any, error) {
	if action == nil {
		return "", nil, nil
	}
	msg := commandMessageFromAction(s.scope, action, sessionKey, rawCommand)
	if s.capture != nil {
		return s.capture.CaptureCommandOutput(strings.TrimSpace(action.MessageID), func() error {
			if s.handleCommand == nil {
				return nil
			}
			return s.handleCommand(msg, rawCommand)
		})
	}
	if s.handleCommand == nil {
		return "", nil, nil
	}
	return "", nil, s.handleCommand(msg, rawCommand)
}

func (s MenuCommandService) Complete(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	parentAction = textutil.FirstNonEmpty(actionStringValue(action, "parent_action"), strings.TrimSpace(parentAction))
	text, card, err := s.run(action, sessionKey, rawCommand)
	if err != nil {
		resp := &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
		}
		if fallback, ok := s.fallback(parentAction, sessionKey); ok {
			resp.Card = rawCard(fallback)
		}
		return resp, nil
	}
	if card != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "已执行 " + rawCommand},
			Card:  rawCard(card),
		}, nil
	}
	resp := &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: textutil.FirstNonEmpty(text, "已执行 "+rawCommand)},
	}
	if fallback, ok := s.fallback(parentAction, sessionKey); ok {
		resp.Card = rawCard(fallback)
	}
	return resp, nil
}

func (s MenuCommandService) fallback(actionName, sessionKey string) (map[string]any, bool) {
	if s.renderFallback == nil {
		return nil, false
	}
	return s.renderFallback(actionName, sessionKey)
}
