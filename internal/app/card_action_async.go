package app

import (
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func callbackResponseCard(resp *callback.CardActionTriggerResponse) map[string]any {
	if resp == nil || resp.Card == nil {
		return nil
	}
	card, _ := resp.Card.Data.(map[string]any)
	return card
}

func callbackResponseToastText(resp *callback.CardActionTriggerResponse) string {
	if resp == nil || resp.Toast == nil {
		return ""
	}
	return strings.TrimSpace(resp.Toast.Content)
}

func completeAsyncCommandAction(
	a *App,
	action *feishu.CardAction,
	sessionKey, rawCommand, fallbackAction, toastText string,
	preparingCard map[string]any,
	successCardFromText func(sessionKey, text string) map[string]any,
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return completeMenuCommand(a, action, sessionKey, rawCommand, fallbackAction)
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		text, card, err := runCommandFromCardAction(a, action, sessionKey, rawCommand)
		switch {
		case err != nil:
			card = failureCard(sessionKey, err.Error())
		case card != nil:
		case successCardFromText != nil:
			card = successCardFromText(sessionKey, strings.TrimSpace(text))
		default:
			card = failureCard(sessionKey, firstNonEmpty(strings.TrimSpace(text), "命令没有返回卡片"))
		}
		patchMaintenanceCard(a, messageID, card, patchWarnMsg,
			"session_key", sessionKey,
			"message_id", messageID,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(preparingCard),
	}, nil
}

func completeAsyncRenderedCardAction(
	a *App,
	action *feishu.CardAction,
	sessionKey, toastText string,
	preparingCard map[string]any,
	run func() (*callback.CardActionTriggerResponse, error),
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return run()
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		resp, err := run()
		card := callbackResponseCard(resp)
		if card == nil {
			errText := callbackResponseToastText(resp)
			if err != nil {
				errText = err.Error()
			}
			card = failureCard(sessionKey, firstNonEmpty(strings.TrimSpace(errText), "操作没有返回卡片"))
		}
		patchMaintenanceCard(a, messageID, card, patchWarnMsg,
			"session_key", sessionKey,
			"message_id", messageID,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(preparingCard),
	}, nil
}
