package feishuapp

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/backend/codex"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	"feidex/internal/application/backendops"
	"feidex/internal/codexrpc"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	appruntime "feidex/internal/runtime"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Composition explicitly binds input families to owners. Backend event
// decoding has already completed when Dispatch is called.
func newInputDispatcher(a *App) application.Dispatcher {
	owner := a.runtimeOwner
	frontendID := identity.FrontendID(a.FrontendID())
	runner := newEffectRunner(owner)
	inThread := a.configView().replyInThreadEnabled()
	router := newFeishuEventRouter(a.started, a.bindings.Inbound, owner.InboundDeduper, owner, func(msg *feishu.InboundMessage, err error) {
		if msg == nil || err == nil {
			return
		}
		_ = runner.Run(owner.Lifecycle.Context(), []application.Effect{application.SendMessage{
			Frontend:       frontendID,
			Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
			ReplyMessageID: msg.MessageID,
			Text:           "执行失败: " + err.Error(),
			InThread:       inThread,
		}})
	})
	cardActions := cardActionDispatcher{inner: a.bindings.CardActions}
	backendEvents := a.bindings.BackendEvents
	return application.NewDispatcher(identity.FrontendID(a.FrontendID()), application.Handlers{
		Message: func(_ context.Context, event application.MessageReceived) (application.Result, error) {
			router.handleMessage(&event.Message)
			return application.Result{}, nil
		},
		Card: func(_ context.Context, event application.CardActionReceived) (application.Result, error) {
			action := fromApplicationCardAction(event.Action)
			response, err := cardActions.dispatch(action)
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
			a.bindings.AutoRetry.RunAutoRetryTimer(string(event.SessionKey), event.Sequence)
			return application.Result{}, nil
		},
		Backend: func(ctx context.Context, event application.BackendEventReceived) (application.Result, error) {
			return backendEvents.Handle(ctx, event.Event)
		},
	})
}
func dispatchInput(d BackendRuntimeDeps, input application.Input) (application.Result, error) {
	dispatcher := d.dispatcher
	var result application.Result
	var err error
	if d.sessionActors == nil {
		result, err = dispatcher.Dispatch(context.Background(), input)
	} else {
		d.sessionActors.Run(application.SessionActorKey(input), func() {
			result, err = dispatcher.Dispatch(d.contextFn(), input)
		})
	}
	if err != nil {
		return result, err
	}
	return result, newEffectRunner(d.runtime.ensureRuntimeOwner()).Run(d.contextFn(), result.Effects)
}

func dispatchBackendEvent(d BackendRuntimeDeps, event application.BackendEvent) {
	if _, err := dispatchInput(d, application.BackendEventReceived{
		Frontend:   identity.FrontendID(d.frontendID),
		SessionKey: identity.SessionKey(sessionKeyForBackendEvent(d, event)),
		Event:      event,
	}); err != nil {
		slog.Error("backend event dispatch failed", "kind", event.Kind, "error", err)
	}
}

func sessionKeyForBackendEvent(d BackendRuntimeDeps, event application.BackendEvent) string {
	threadID := strings.TrimSpace(event.ThreadID)
	if threadID == "" {
		return ""
	}
	return d.conversationQuery.SessionForBackendThread(threadID)
}
func dispatchCodexNotification(d BackendRuntimeDeps, method string, params json.RawMessage) {
	event, handled, err := codex.DecodeNotification(method, params)
	if err != nil {
		slog.Warn("invalid backend notification", "method", method, "error", err)
		return
	}
	if handled {
		dispatchBackendEvent(d, event)
	}
}
func dispatchCodexRequest(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	dispatchBackendEvent(d, codex.DecodeRequest(req))
}

func newEffectRunner(runtimeowner *appruntime.FrontendOwner) appruntime.EffectRunner {
	return *runtimeowner.EffectRunner
}

func buildEffectRunner(a *App) appruntime.EffectRunner {
	if a == nil {
		return appruntime.EffectRunner{}
	}
	transport := a.feishu
	if a.transport != nil {
		transport = a.transport
	}
	runner := feishuoutbound.NewEffectRunner(transport)
	if owner := a.runtimeView().ensureRuntimeOwner(); owner != nil {
		runner.Deduper = owner.EffectDeduper
	}
	runner.Save = func(ctx context.Context, e application.SaveState) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if string(e.Frontend) != a.FrontendID() {
			return fmt.Errorf("state effect frontend mismatch")
		}
		return a.State().SaveSession(e.Session)
	}
	runner.StartWithResult = func(ctx context.Context, e application.StartTurn) (backendops.TurnResult, error) {
		if string(e.Frontend) != a.FrontendID() {
			return backendops.TurnResult{}, fmt.Errorf("turn effect frontend mismatch")
		}
		client, err := requireCodexGateway(a)
		if err != nil {
			return backendops.TurnResult{}, err
		}
		return client.StartTurn(ctx, e.Request)
	}
	runner.Resolve = func(ctx context.Context, e application.ResolveBackendRequest) error {
		if string(e.Frontend) != a.FrontendID() {
			return fmt.Errorf("response effect frontend mismatch")
		}
		if e.Backend != domainbackend.BackendCodex {
			return fmt.Errorf("unsupported response backend %q", e.Backend)
		}
		client, err := a.runtimeView().requireCodexClient()
		if err != nil {
			return err
		}
		return codex.Respond(ctx, client, e.Response)
	}
	runner.Steer = func(ctx context.Context, e application.SteerTurn) error {
		if string(e.Frontend) != a.FrontendID() {
			return fmt.Errorf("steer effect frontend mismatch")
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		gateway, err := requireCodexGateway(a)
		if err != nil {
			return err
		}
		return gateway.SteerTurn(ctx, e.ThreadID, e.ExpectedTurnID, &domainsubmission.Submission{InputText: strings.TrimSpace(e.Text)})
	}
	runner.Enqueue = func(ctx context.Context, e application.EnqueueInput) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if string(e.Frontend) != a.FrontendID() {
			return fmt.Errorf("enqueue effect frontend mismatch")
		}
		return a.bindings.Submissions.EnqueueSubmission(&e.Message, e.SessionKey, e.BindOnlyCurrentRoot)
	}
	runner.RefreshGroup = func(ctx context.Context, e application.RefreshGroupStatus) error {
		if string(e.Frontend) != a.FrontendID() {
			return fmt.Errorf("group refresh effect frontend mismatch")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		scheduleGroupAnnouncementStatusRefresh(a, e.ChatID, e.Reason)
		return nil
	}
	return runner
}
func dispatchCardAction(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	result, err := dispatchInput(a.BackendRuntimeDeps(), application.CardActionReceived{Frontend: identity.FrontendID(a.FrontendID()), Action: toApplicationCardAction(action)})
	if err != nil {
		return nil, err
	}
	response, _ := result.Response.(*callback.CardActionTriggerResponse)
	return response, nil
}
