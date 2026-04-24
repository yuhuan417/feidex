package app

import (
	"fmt"
	"log/slog"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type cardActionService struct {
	app      *App
	handlers map[string]cardActionHandler
}

func newCardActionService(app *App) cardActionService {
	return cardActionService{app: app, handlers: cardActionHandlers}
}

func (s cardActionService) dispatch(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	name := resolvedCardActionName(action)
	if reason := s.app.backendSwitchBlocksCardAction(name); reason != "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: reason},
		}, nil
	}
	handler := s.handlers[name]
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
	return handler(s.app, action)
}
