package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/goalcmd"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	goalapp "feidex/internal/application/goal"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"log/slog"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type goalOutbound struct {
	frontend identity.FrontendID
	runner   frontendruntime.EffectRunner
}

func (o goalOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, ReplyMessageID: messageID,
		View: feishuoutbound.Card(card), InThread: inThread,
	})
}

func (o goalOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return o.runner.Run(ctx, []application.Effect{application.SendMessage{
		Frontend: o.frontend, ReplyMessageID: messageID, Text: text, InThread: inThread,
	}})
}

func (o goalOutbound) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, Chat: identity.ChatRef{ID: chatID}, View: feishuoutbound.Card(card),
	})
}

func GoalCommandOutbound(frontend identity.FrontendID, runner frontendruntime.EffectRunner) goalcmd.Outbound {
	return goalOutbound{frontend: frontend, runner: runner}
}

func commandGoalRaw(goalcommands *goalcmd.Service, msg *feishu.InboundMessage, raw string, args []string) error {
	return goalcommands.CommandGoal(msg, raw, args)
}

func goalTrackerForApp(tracker *goalapp.Tracker) *goalapp.Tracker {
	return tracker
}

func RequireCodexGoalGateway(a *App) (goalapp.Gateway, error) { return requireCodexGateway(a) }

func GoalCommandSessionKey(frontendID string, msg *feishu.InboundMessage) string {
	return (frontendConfigView{frontendID: frontendID}).makeSessionKey(msg)
}

func CompleteGoalMenuCommand(a *App, action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
}

type goalAnchorPresenter struct{ outbound goalcmd.Outbound }

func (p goalAnchorPresenter) SendContinuationAnchor(ctx context.Context, chatID string, goal conversation.ThreadGoal, ordinal int) (string, error) {
	return p.outbound.SendCard(ctx, chatID, goalcmd.RenderContinuationCard(goal, ordinal))
}

func GoalContinuationPorts(a *App) goalapp.Dependencies {
	owner := a.runtimeOwner
	return goalapp.Dependencies{
		Context: a.Context, Repository: a.State(), Tracker: a.bindings.Goals,
		Presenter: goalAnchorPresenter{outbound: goalOutbound{frontend: identity.FrontendID(a.FrontendID()), runner: newEffectRunner(a.runtimeOwner)}},
		Bindings:  owner.TurnBindings, Replies: a.bindings.Continuation,
		Streams: a.bindings.TurnPresentation, Live: turnRuntimePort{lifecycle: &owner.Lifecycle, asyncRunner: a.asyncRunner, liveThreads: liveThreadMarker{
			tracker: owner.LiveThreads, state: a.State(), announcement: a.bindings.AnnouncementQuery, refreshes: owner.Announcements,
		}},
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
