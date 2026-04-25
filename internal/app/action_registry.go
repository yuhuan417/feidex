package app

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

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
	if reason := newRuntimeStateService(s.app).backendSwitchBlocksCardAction(name); reason != "" {
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
	return handler(s, action)
}

type cardActionHandler func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)

// Card action handlers run on the Feishu callback ack path.
// Keep them fast: validate input, persist state, enqueue work, and return.
// Do not put clone/download/fetch/review/upgrade or other blocking workflows
// directly in these handlers.

var cardActionHandlers = mergeCardActionHandlerSets(
	menuCardActionHandlers,
	workspaceCardActionHandlers,
	maintenanceCardActionHandlers,
	pendingCardActionHandlers,
)

func mergeCardActionHandlerSets(sets ...map[string]cardActionHandler) map[string]cardActionHandler {
	merged := make(map[string]cardActionHandler)
	for _, set := range sets {
		for name, handler := range set {
			merged[name] = handler
		}
	}
	return merged
}

func resolvedCardActionName(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	name, _ := action.ActionValue["action"].(string)
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(action.Name)
}

func actionSessionKey(action *feishu.CardAction) string {
	return actionStringValue(action, "session_key")
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}

func actionIntValue(action *feishu.CardAction, key string) int {
	if action == nil {
		return 0
	}
	switch value := action.ActionValue[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func actionIndexOption(action *feishu.CardAction, warning string) (*callback.CardActionTriggerResponse, int, bool) {
	index, err := strconv.Atoi(strings.TrimSpace(action.Option))
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: warning}}, 0, false
	}
	return nil, index, true
}
