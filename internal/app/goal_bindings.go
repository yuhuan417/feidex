package app

import (
	"context"
	"feidex/internal/app/goalcmd"
	"feidex/internal/codexrpc"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	"log/slog"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	goalCommandUsage          = goalcmd.CommandUsage
	goalMaxObjectiveRunes     = goalcmd.MaxObjectiveRunes
	goalSubmissionKind        = goalcmd.SubmissionKind
	goalContinuationInputText = goalcmd.ContinuationInputText
)

func goalTrackerForApp(a *App) *goalcmd.Tracker {
	if a == nil {
		return nil
	}
	if a.trackers.goals == nil {
		a.trackers.goals = goalcmd.NewTracker()
	}
	return a.trackers.goals
}

func commandGoalRaw(a *App, msg *feishu.InboundMessage, raw string, args []string) error {
	return newGoalService(a).CommandGoal(msg, raw, args)
}

func newGoalService(a *App) goalcmd.Service {
	return goalcmd.NewService(goalDependenciesForApp(a))
}

func goalDependenciesForApp(a *App) goalcmd.Dependencies {
	if a == nil {
		return goalcmd.Dependencies{}
	}
	return goalcmd.Dependencies{
		StateProvider: a.State(), FeishuClient: a.feishu,
		CodexClientProvider: func() (goalcmd.CodexClient, error) { return requireCodexClient(a) }, GoalTracker: goalTrackerForApp(a),
		MakeSessionKeyFn: func(m *feishu.InboundMessage) string { return makeSessionKey(a, m) }, ReplyInThreadEnabledFn: func(v string) bool { return replyInThreadEnabled(a, v) },
		MenuCardBodyForSessionFn: func(s, x, b string) string { return menuCardBodyForSession(a, s, x, b) }, ActionStringValueFn: actionStringValue, ActionSessionKeyFn: actionSessionKey,
		CompleteMenuCommandFn: func(x *feishu.CardAction, s, r, f string) (*callback.CardActionTriggerResponse, error) {
			return completeMenuCommand(a, x, s, r, f)
		}, DefaultWorkspaceIDFn: func() string { return defaultWorkspaceID(a) }, SessionBelongsToFrontendFn: func(s string) bool { return sessionBelongsToFrontend(a, s) },
		BindTurnSubmissionFn: func(t, u, s, i string) { newRuntimeStateService(a).BindTurnSubmission(t, u, s, i) }, MarkTurnStartedAtFn: func(t string, v time.Time) { newRuntimeStateService(a).MarkTurnStartedAt(t, v) },
		RecordSubmissionSourceLinksFn: func(s *domainsubmission.Submission) { newReplyContinuationService(a).RecordSubmissionSourceLinks(s) }, RecordRootTurnBindingFn: func(r, s, t, u string) { newReplyContinuationService(a).RecordRootTurnBinding(r, s, t, u) },
		NoteTurnStartedFn: func(s string, sub *domainsubmission.Submission) { newTurnStreamService(a).NoteTurnStarted(s, sub) }, MarkSessionThreadLiveFn: func(s, t string) { markSessionThreadLive(a, s, t) }, ContextProvider: a,
	}
}

func onThreadGoalUpdated(a *App, note codexrpc.ThreadGoalUpdatedNotification) {
	goalcmd.OnThreadGoalUpdated(goalDependenciesForApp(a), note)
}

func onThreadGoalCleared(a *App, note codexrpc.ThreadGoalClearedNotification) {
	goalcmd.OnThreadGoalCleared(goalDependenciesForApp(a), note)
}

func completeMenuGoalAsync(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return newGoalService(a).CompleteMenuGoal(action, sessionKey)
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		resp, err := newGoalService(a).CompleteMenuGoal(action, sessionKey)
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
		return run(newGoalService(a))
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		resp, err := run(newGoalService(a))
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
	if replyErr := a.feishu.ReplyText(context.Background(), messageID, text, goalActionReplyInThread(a, sessionKey)); replyErr != nil {
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
		return replyInThreadEnabled(a, sess.ChatType)
	}
	return false
}
