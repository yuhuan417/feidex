package app

import (
	"fmt"
	"strconv"
	"strings"

	"feidex/internal/application"
	appcardaction "feidex/internal/application/cardaction"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type cardActionService struct {
	app      *App
	handlers map[string]cardActionHandler
	inner    appcardaction.Service
}

func newCardActionService(app *App) cardActionService {
	handlers := cardActionHandlers()
	bound := make(map[string]appcardaction.Handler, len(handlers))
	for name, handler := range handlers {
		h := handler
		bound[name] = func(action application.CardAction) (any, error) {
			response, err := h(cardActionService{app: app, handlers: handlers}, fromApplicationCardAction(action))
			return response, err
		}
	}
	service := cardActionService{app: app, handlers: handlers}
	service.inner = appcardaction.NewService(appcardaction.Dependencies{
		NormalizeSessionKey: func(action *application.CardAction) {
			converted := fromApplicationCardAction(*action)
			service.normalizeSessionKey(converted)
			*action = toApplicationCardAction(converted)
		},
		ResolveActionName: resolvedApplicationCardActionName,
		BlockedReason: func(name string) string {
			return newRuntimeStateService(app).backendSwitchBlocksCardAction(name)
		},
		Handlers: bound,
	})
	return service
}

func (s cardActionService) dispatch(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	response, err := s.inner.Dispatch(toApplicationCardAction(action))
	if response == nil {
		return &callback.CardActionTriggerResponse{}, err
	}
	if neutral, ok := response.(application.CardActionResult); ok {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: neutral.ToastType, Content: neutral.ToastContent}}, err
	}
	typed, ok := response.(*callback.CardActionTriggerResponse)
	if !ok {
		return nil, fmt.Errorf("invalid card action response %T", response)
	}
	return typed, err
}

func toApplicationCardAction(action *feishu.CardAction) application.CardAction {
	if action == nil {
		return application.CardAction{}
	}
	return application.CardAction{ActionValue: action.ActionValue, FormValue: action.FormValue, UserID: action.UserID, ChatID: action.ChatID, MessageID: action.MessageID, Name: action.Name, Option: action.Option, InputValue: action.InputValue, Options: action.Options, Checked: action.Checked}
}

func fromApplicationCardAction(action application.CardAction) *feishu.CardAction {
	return &feishu.CardAction{ActionValue: action.ActionValue, FormValue: action.FormValue, UserID: action.UserID, ChatID: action.ChatID, MessageID: action.MessageID, Name: action.Name, Option: action.Option, InputValue: action.InputValue, Options: action.Options, Checked: action.Checked}
}

func resolvedApplicationCardActionName(action application.CardAction) string {
	name, _ := action.ActionValue["action"].(string)
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(action.Name)
}

func (s cardActionService) normalizeSessionKey(action *feishu.CardAction) {
	if s.app == nil || action == nil || action.ActionValue == nil {
		return
	}
	raw, ok := action.ActionValue["session_key"].(string)
	if !ok {
		return
	}
	if normalized := normalizeSessionKey(s.app, raw); normalized != strings.TrimSpace(raw) {
		action.ActionValue["session_key"] = normalized
	}
}

type cardActionHandler func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)

// Card action handlers run on the Feishu callback ack path.
// Keep them fast: validate input, persist state, enqueue work, and return.
// Do not put clone/download/fetch/review/upgrade or other blocking workflows
// directly in these handlers.

func cardActionHandlers() map[string]cardActionHandler {
	return mergeCardActionHandlerSets(
		menuCardActionHandlers(),
		workspaceCardActionHandlers(),
		maintenanceCardActionHandlers(),
		pendingCardActionHandlers(),
	)
}

func mergeCardActionHandlerSets(sets ...map[string]cardActionHandler) map[string]cardActionHandler {
	merged := make(map[string]cardActionHandler)
	for _, set := range sets {
		for name, handler := range set {
			merged[name] = handler
		}
	}
	return merged
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
