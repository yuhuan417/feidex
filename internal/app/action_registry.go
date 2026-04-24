package app

import (
	"strconv"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

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
