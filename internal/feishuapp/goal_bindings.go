package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/goalcmd"
	goalapp "feidex/internal/application/goal"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	"log/slog"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type goalOutbound struct{ app *App }

func (o goalOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

func (o goalOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return replyTextByAnchorEffect(ctx, o.app, messageID, text, inThread)
}

func (o goalOutbound) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	return sendCardWithIDEffect(ctx, o.app, chatID, card)
}

type goalCardRenderer struct{ app *App }

func (r goalCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

func goalTrackerForApp(tracker *goalapp.Tracker) *goalapp.Tracker {
	return tracker
}

func commandGoalRaw(goalcommands *goalcmd.Service, msg *feishu.InboundMessage, raw string, args []string) error {
	return goalcommands.CommandGoal(msg, raw, args)
}

func GoalCommandPorts(a *App) goalcmd.Dependencies { return goalDependenciesForApp(a) }

func RequireCodexGoalGateway(a *App) (goalapp.Gateway, error) { return requireCodexGateway(a) }

func goalDependenciesForApp(a *App) goalcmd.Dependencies {
	if a == nil {
		return goalcmd.Dependencies{}
	}
	return goalcmd.Dependencies{
		StateProvider: a.State(), Outbound: goalOutbound{app: a}, CardRenderer: goalCardRenderer{app: a}, GoalManagement: a.bindings.GoalManagement,
		GoalTracker:      goalTrackerForApp(a.bindings.Goals),
		MakeSessionKeyFn: func(m *feishu.InboundMessage) string { return a.configView().makeSessionKey(m) }, ReplyInThreadEnabledFn: func(v string) bool { return a.configView().replyInThreadEnabled() },
		MenuCardBodyForSessionFn: func(s, x, b string) string { return menuCardBodyForSession(a, s, x, b) }, ActionStringValueFn: actionStringValue, ActionSessionKeyFn: actionSessionKey,
		CompleteMenuCommandFn: func(x *feishu.CardAction, s, r, f string) (*callback.CardActionTriggerResponse, error) {
			return completeMenuCommand(a, x, s, r, f)
		}, ContextProvider: a,
	}
}

type goalAnchorPresenter struct{ outbound goalcmd.Outbound }

func (p goalAnchorPresenter) SendContinuationAnchor(ctx context.Context, chatID string, goal conversation.ThreadGoal, ordinal int) (string, error) {
	return p.outbound.SendCard(ctx, chatID, goalcmd.RenderContinuationCard(goal, ordinal))
}

func GoalContinuationPorts(a *App) goalapp.Dependencies {
	return goalapp.Dependencies{
		Context: a.Context, Repository: a.State(), Tracker: goalTrackerForApp(a.bindings.Goals),
		Presenter: goalAnchorPresenter{outbound: goalOutbound{app: a}},
		Bindings:  a.runtimeOwner.TurnBindings, Replies: a.bindings.Continuation,
		Streams: a.bindings.TurnPresentation, Live: turnRuntimePort{app: a},
		DefaultWorkspaceID: func() string { return a.configView().defaultWorkspaceID() },
		BelongsToFrontend:  func(key string) bool { return a.configView().sessionBelongsToFrontend(key) },
	}
}

func completeMenuGoalAsync(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return a.bindings.GoalCommands.CompleteMenuGoal(action, sessionKey)
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		resp, err := a.bindings.GoalCommands.CompleteMenuGoal(action, sessionKey)
		completeGoalAsyncResult(a, action, sessionKey, messageID, resp, err, "goal menu patch failed")
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在处理 goal"},
	}, nil
}

func completeGoalRenderedActionAsync(
	a *App,
	action *feishu.CardAction,
	sessionKey, toastText string,
	run func(goalcmd.Service) (*callback.CardActionTriggerResponse, error),
) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return run(*a.bindings.GoalCommands)
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		resp, err := run(*a.bindings.GoalCommands)
		completeGoalAsyncResult(a, action, sessionKey, messageID, resp, err, "goal action patch failed")
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
	}, nil
}

func completeGoalAsyncResult(a *App, action *feishu.CardAction, sessionKey, messageID string, resp *callback.CardActionTriggerResponse, err error, patchWarnMsg string) {
	if a == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	if card := callbackResponseCard(resp); card != nil {
		patchMaintenanceCard(a, messageID, card, patchWarnMsg,
			"session_key", sessionKey,
			"message_id", messageID,
		)
		return
	}
	text := callbackResponseToastText(resp)
	if err != nil {
		text = err.Error()
	}
	text = strings.TrimSpace(text)
	if text == "" || a.feishu == nil {
		return
	}
	if replyErr := replyTextByAnchorEffect(context.Background(), a, messageID, text, goalActionReplyInThread(a, sessionKey)); replyErr != nil {
		slog.Warn("goal async text reply failed",
			"session_key", sessionKey,
			"message_id", messageID,
			"error", replyErr,
		)
	}
}

func goalActionReplyInThread(a *App, sessionKey string) bool {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return false
	}
	if sess := a.State().Session(sessionKey); sess != nil {
		return a.configView().replyInThreadEnabled()
	}
	return false
}
