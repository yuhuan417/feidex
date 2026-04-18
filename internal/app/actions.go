package app

import (
	"fmt"
	"log/slog"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// dispatchCardAction is the synchronous Feishu callback entrypoint.
// Handlers must acknowledge quickly and must not block on heavy workflows.
func (a *App) dispatchCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	name := resolvedCardActionName(action)
	handler := cardActionHandlers[name]
	if handler == nil {
		slog.Warn("unknown feishu card action",
			"name", name,
			"raw_name", action.Name,
			"message_id", action.MessageID,
			"chat_id", action.ChatID,
			"user_id", action.UserID,
			"action_value", fmt.Sprintf("%v", action.ActionValue),
			"form_value", fmt.Sprintf("%v", action.FormValue),
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "未知操作"},
		}, nil
	}
	return handler(a, action)
}
