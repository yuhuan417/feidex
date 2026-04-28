package app

import (
	"context"
	"strings"

	appfeishuwrap "feidex/internal/app/feishuwrap"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func parseSessionKeyMeta(sessionKey string) (chatType, chatID, rootMessageID, userID string) {
	_, chatType, chatID, rootMessageID, userID = parseSessionKey(sessionKey)
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

func replyCommandActionResponse(a *App, msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	if msg == nil || resp == nil {
		return nil
	}
	replyInThread := replyInThreadEnabled(a, msg.ChatType)
	if resp.Card != nil {
		if card, ok := resp.Card.Data.(map[string]any); ok && len(card) > 0 {
			_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThread)
			return err
		}
	}
	if resp.Toast != nil && strings.TrimSpace(resp.Toast.Content) != "" {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, strings.TrimSpace(resp.Toast.Content), replyInThread)
	}
	return nil
}

func commandMessageFromAction(a *App, action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
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
	if sess := a.State().Session(sessionKey); sess != nil {
		msg.ChatID = firstNonEmpty(msg.ChatID, strings.TrimSpace(sess.ChatID))
		msg.ChatType = firstNonEmpty(msg.ChatType, strings.TrimSpace(sess.ChatType))
		msg.UserID = firstNonEmpty(msg.UserID, strings.TrimSpace(sess.OwnerUserID))
	}
	if msg.ChatType == "group" && strings.TrimSpace(msg.RootMessageID) == "" {
		msg.RootMessageID = firstNonEmpty(rootMessageID, msg.MessageID)
	}
	return msg
}

func runCommandFromCardAction(a *App, action *feishu.CardAction, sessionKey, rawCommand string) (string, map[string]any, error) {
	if action == nil {
		return "", nil, nil
	}
	msg := commandMessageFromAction(a, action, sessionKey, rawCommand)
	if capture, ok := a.feishu.(appfeishuwrap.CommandCaptureFeishuClient); ok {
		return capture.CaptureCommandOutput(strings.TrimSpace(action.MessageID), func() error {
			return handleCommand(a, msg, rawCommand)
		})
	}
	return "", nil, handleCommand(a, msg, rawCommand)
}

func completeMenuCommand(a *App, action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	parentAction = firstNonEmpty(actionStringValue(action, "parent_action"), strings.TrimSpace(parentAction))
	text, card, err := runCommandFromCardAction(a, action, sessionKey, rawCommand)
	if err != nil {
		resp := &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
		}
		if fallback, ok := renderMenuCommandFallback(a, parentAction, sessionKey); ok {
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
		Toast: &callback.Toast{Type: "success", Content: firstNonEmpty(text, "已执行 "+rawCommand)},
	}
	if fallback, ok := renderMenuCommandFallback(a, parentAction, sessionKey); ok {
		resp.Card = rawCard(fallback)
	}
	return resp, nil
}

func renderMenuCommandFallback(a *App, actionName, sessionKey string) (map[string]any, bool) {
	if a == nil || a.cfg == nil || len(a.cfg.Workspaces) == 0 {
		return nil, false
	}
	return newMenuActionService(a).renderMenuNodeCard(actionName, sessionKey)
}
