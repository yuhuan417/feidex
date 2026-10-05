package feishuapp

import (
	"fmt"
	"strconv"
	"strings"

	claudesupport "feidex/internal/adapter/feishu/claudesupport"
	history "feidex/internal/adapter/feishu/history"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/adapter/feishu/threadmenu"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/application"
	appcardaction "feidex/internal/application/cardaction"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type cardActionService struct {
	app *App
}

type cardActionDispatcher struct {
	inner appcardaction.Service
}

func CardActionPorts(app *App, normalizeSessionKey func(string) string, blockedReason func(string) string, workspaceDeleteActions workspacecmd.WorkspaceDeleteActions, historyService history.Service, serverRequests *serverrequest.Service, claudeSupport *claudesupport.Service, reviewCommands appreviewcmd.ReviewFormService, upgrades appupgradecmd.UpgradeService, backendUpgrades backendUpgradeService, pathPicker PathPickerActionInputs, threadMenu *threadmenu.Service) appcardaction.Dependencies {
	appHandlers := mergeCardActionHandlerSets(
		menuCardActionHandlers(),
		workspaceCardActionHandlers(),
		maintenanceCardActionHandlers(),
		pendingCardActionHandlers(),
	)
	portHandlers := mergeCardActionPortHandlerSets(
		maintenancePortCardActionHandlers(upgrades, backendUpgrades, backendUpgradeCommandCompleter(cardActionService{app: app})),
		pendingPortCardActionHandlers(serverRequests, claudeSupport, reviewCommands),
		workspaceDeletePortCardActionHandlers(workspaceDeleteActions),
		historyCardActionHandlers(historyService),
		serverRequestCardActionHandlers(serverRequests),
		pathPickerActionHandlers(pathPicker),
		threadMenuPortCardActionHandlers(threadMenu),
	)
	bound := bindAppCardActionHandlers(cardActionService{app: app}, appHandlers)
	for name, handler := range bindCardActionPortHandlers(portHandlers) {
		bound[name] = handler
	}
	return appcardaction.Dependencies{
		NormalizeSessionKey: func(action *application.CardAction) {
			if action == nil {
				return
			}
			raw, ok := action.ActionValue.String("session_key")
			if !ok {
				return
			}
			if normalized := normalizeSessionKey(raw); normalized != strings.TrimSpace(raw) {
				action.ActionValue.SetString("session_key", normalized)
			}
		},
		ResolveActionName: resolvedApplicationCardActionName,
		BlockedReason:     blockedReason,
		Handlers:          bound,
	}
}

func bindAppCardActionHandlers(service cardActionService, handlers map[string]cardActionHandler) map[string]appcardaction.Handler {
	bound := make(map[string]appcardaction.Handler, len(handlers))
	for name, handler := range handlers {
		h := handler
		bound[name] = func(action application.CardAction) (any, error) {
			response, err := h(service, fromApplicationCardAction(action))
			return response, err
		}
	}
	return bound
}

func bindCardActionPortHandlers(handlers map[string]cardActionPortHandler) map[string]appcardaction.Handler {
	bound := make(map[string]appcardaction.Handler, len(handlers))
	for name, handler := range handlers {
		h := handler
		bound[name] = func(action application.CardAction) (any, error) {
			return h(fromApplicationCardAction(action))
		}
	}
	return bound
}

func (s cardActionDispatcher) dispatch(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	normalized := toApplicationCardAction(action)
	response, err := s.inner.Dispatch(normalized)
	// Preserve adapter-visible normalization (notably legacy session keys) for
	// callers that retain the callback value after dispatch.
	action.ActionValue = normalized.ActionValue.Map()
	action.FormValue = normalized.FormValue.Map()
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
	return application.CardAction{ActionValue: application.ValuesFromMap(action.ActionValue), FormValue: application.ValuesFromMap(action.FormValue), UserID: action.UserID, ChatID: action.ChatID, MessageID: action.MessageID, Name: action.Name, Option: action.Option, InputValue: action.InputValue, Options: action.Options, Checked: action.Checked}
}

func fromApplicationCardAction(action application.CardAction) *feishu.CardAction {
	return &feishu.CardAction{ActionValue: action.ActionValue.Map(), FormValue: action.FormValue.Map(), UserID: action.UserID, ChatID: action.ChatID, MessageID: action.MessageID, Name: action.Name, Option: action.Option, InputValue: action.InputValue, Options: action.Options, Checked: action.Checked}
}

func resolvedApplicationCardActionName(action application.CardAction) string {
	name, _ := action.ActionValue.String("action")
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(action.Name)
}

type cardActionHandler func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)
type cardActionPortHandler func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)

// Card action handlers run on the Feishu callback ack path.
// Keep them fast: validate input, persist state, enqueue work, and return.
// Do not put clone/download/fetch/review/upgrade or other blocking workflows
// directly in these handlers.

func serverRequestCardActionHandlers(service *serverrequest.Service) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"user_input.answer": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteUserInputAnswer(action)
		},
		"user_input.toggle_multi": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteUserInputMultiToggle(action)
		},
		"elicitation_form.answer": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteElicitationFormAnswer(action)
		},
		"elicitation_form.toggle_multi": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteElicitationMultiToggle(action)
		},
		"approval.command.accept":             serverRequestApprovalAction(service, "approval.command.accept"),
		"approval.command.accept_session":     serverRequestApprovalAction(service, "approval.command.accept_session"),
		"approval.command.decline":            serverRequestApprovalAction(service, "approval.command.decline"),
		"approval.command.cancel":             serverRequestApprovalAction(service, "approval.command.cancel"),
		"approval.file.accept":                serverRequestApprovalAction(service, "approval.file.accept"),
		"approval.file.accept_session":        serverRequestApprovalAction(service, "approval.file.accept_session"),
		"approval.file.decline":               serverRequestApprovalAction(service, "approval.file.decline"),
		"approval.file.cancel":                serverRequestApprovalAction(service, "approval.file.cancel"),
		"approval.permissions.accept_turn":    serverRequestApprovalAction(service, "approval.permissions.accept_turn"),
		"approval.permissions.accept_session": serverRequestApprovalAction(service, "approval.permissions.accept_session"),
		"elicitation_url.accept":              serverRequestElicitationAction(service, "elicitation_url.accept"),
		"elicitation_url.decline":             serverRequestElicitationAction(service, "elicitation_url.decline"),
		"elicitation_url.cancel":              serverRequestElicitationAction(service, "elicitation_url.cancel"),
	}
}

func serverRequestApprovalAction(service *serverrequest.Service, name string) cardActionPortHandler {
	return func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return service.CompleteApprovalAction(action, name)
	}
}

func serverRequestElicitationAction(service *serverrequest.Service, name string) cardActionPortHandler {
	return func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return service.CompleteElicitationURLAction(action, name)
	}
}

func mergeCardActionPortHandlerSets(sets ...map[string]cardActionPortHandler) map[string]cardActionPortHandler {
	merged := make(map[string]cardActionPortHandler)
	for _, set := range sets {
		for name, handler := range set {
			merged[name] = handler
		}
	}
	return merged
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

func GoalCommandActionSessionKey(action *feishu.CardAction) string {
	return actionSessionKey(action)
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}

func GoalCommandActionStringValue(action *feishu.CardAction, key string) string {
	return actionStringValue(action, key)
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
