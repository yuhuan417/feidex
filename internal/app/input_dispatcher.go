package app

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/backend/codex"
	"feidex/internal/application"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"fmt"
	"log/slog"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Composition explicitly binds input families to owners. Backend event
// decoding has already completed when Dispatch is called.
func newInputDispatcher(a *App) application.Dispatcher {
	router := newFeishuEventRouter(a)
	cardActions := newCardActionService(a)
	backendEvents := newBackendEventService(a)
	return application.Dispatcher{
		Frontend: identity.FrontendID(a.FrontendID()),
		Message: func(_ context.Context, event application.MessageReceived) (application.Result, error) {
			router.handleMessage(&event.Message)
			return application.Result{}, nil
		},
		Card: func(_ context.Context, event application.CardActionReceived) (application.Result, error) {
			response, err := cardActions.dispatch(&event.Action)
			return application.Result{Response: response}, err
		},
		Recall: func(_ context.Context, event application.MessageRecalled) (application.Result, error) {
			router.handleRecall(&feishu.MessageRecall{MessageID: event.MessageID, ChatID: event.ChatID})
			return application.Result{}, nil
		},
		Reaction: func(_ context.Context, event application.MessageReacted) (application.Result, error) {
			router.handleReaction(&feishu.MessageReaction{MessageID: event.MessageID, ChatID: event.ChatID, UserID: event.UserID, EmojiType: event.EmojiType})
			return application.Result{}, nil
		},
		Retry: func(_ context.Context, event application.RetryTimerFired) (application.Result, error) {
			newAutoRetryService(a).RunAutoRetryTimer(string(event.SessionKey), event.Sequence)
			return application.Result{}, nil
		},
		Backend: func(ctx context.Context, event application.BackendEventReceived) (application.Result, error) {
			return backendEvents.Handle(ctx, event.Event)
		},
	}
}
func dispatchInput(a *App, input application.Input) (application.Result, error) {
	dispatcher := newInputDispatcher(a)
	if a != nil && a.dispatcher != nil {
		dispatcher = *a.dispatcher
	}
	var result application.Result
	var err error
	if a == nil {
		result, err = dispatcher.Dispatch(context.Background(), input)
	} else {
		a.sessionActorRuntime().Run(sessionActorKey(input), func() {
			result, err = dispatcher.Dispatch(a.Context(), input)
		})
	}
	if err != nil {
		return result, err
	}
	return result, newEffectRunner(a).Run(a.Context(), result.Effects)
}

func sessionActorKey(input application.Input) string {
	const transportPrefix = "transport:"
	switch event := input.(type) {
	case application.MessageReceived:
		if key := strings.TrimSpace(event.Message.SessionKey); key != "" {
			return "session:" + key
		}
		return transportPrefix + string(event.Chat.Type) + ":" + strings.TrimSpace(event.Chat.ID)
	case application.CardActionReceived:
		if key, _ := event.Action.ActionValue["session_key"].(string); strings.TrimSpace(key) != "" {
			return "session:" + strings.TrimSpace(key)
		}
		return transportPrefix + strings.TrimSpace(event.Action.ChatID)
	case application.BackendEventReceived:
		if key := strings.TrimSpace(string(event.SessionKey)); key != "" {
			return "session:" + key
		}
		return transportPrefix + "backend"
	case application.RetryTimerFired:
		return "session:" + strings.TrimSpace(string(event.SessionKey))
	case application.MessageRecalled:
		return transportPrefix + strings.TrimSpace(event.ChatID)
	case application.MessageReacted:
		return transportPrefix + strings.TrimSpace(event.ChatID)
	default:
		return transportPrefix + "unknown"
	}
}
func dispatchBackendEvent(a *App, event application.BackendEvent) {
	if _, err := dispatchInput(a, application.BackendEventReceived{
		Frontend:   identity.FrontendID(a.FrontendID()),
		SessionKey: identity.SessionKey(sessionKeyForBackendEvent(a, event)),
		Event:      event,
	}); err != nil {
		slog.Error("backend event dispatch failed", "kind", event.Kind, "error", err)
	}
}

func sessionKeyForBackendEvent(a *App, event application.BackendEvent) string {
	threadID := strings.TrimSpace(event.ThreadID)
	if a == nil || threadID == "" {
		return ""
	}
	for _, sess := range a.State().Sessions() {
		if sess == nil {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == threadID {
			return sess.Key
		}
		for _, lineage := range sess.BackendThreads {
			if strings.TrimSpace(lineage.ThreadID) == threadID {
				return sess.Key
			}
		}
	}
	return ""
}
func dispatchCodexNotification(a *App, method string, params json.RawMessage) {
	event, handled, err := codex.DecodeNotification(method, params)
	if err != nil {
		slog.Warn("invalid backend notification", "method", method, "error", err)
		return
	}
	if handled {
		dispatchBackendEvent(a, event)
	}
}
func dispatchCodexRequest(a *App, req codexrpc.RequestEnvelope) {
	dispatchBackendEvent(a, codex.DecodeRequest(req))
}

func newEffectRunner(a *App) frontendruntime.EffectRunner {
	if a != nil && a.effectRunner != nil {
		return *a.effectRunner
	}
	return frontendruntime.EffectRunner{
		Send: func(ctx context.Context, e application.SendMessage) error {
			if e.ReplyMessageID != "" {
				return a.feishu.ReplyText(ctx, e.ReplyMessageID, e.Text, e.InThread)
			}
			return a.feishu.SendText(ctx, e.Chat.ID, e.Text)
		},
		Patch: func(ctx context.Context, e application.PatchCard) error {
			card, ok := e.View.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid card view %T", e.View)
			}
			return a.feishu.PatchCard(ctx, e.MessageID, card)
		},
	}
}
func dispatchCardAction(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return newCardActionService(a).dispatch(nil)
	}
	result, err := dispatchInput(a, application.CardActionReceived{Frontend: identity.FrontendID(a.FrontendID()), Action: *action})
	if err != nil {
		return nil, err
	}
	response, _ := result.Response.(*callback.CardActionTriggerResponse)
	return response, nil
}
