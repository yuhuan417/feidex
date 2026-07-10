package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/goalcmd"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type goalService = goalcmd.Service
type goalTracker = goalcmd.Tracker
type goalAnchor = goalcmd.Anchor

const (
	goalCommandUsage          = goalcmd.CommandUsage
	goalMaxObjectiveRunes     = goalcmd.MaxObjectiveRunes
	goalSubmissionKind        = goalcmd.SubmissionKind
	goalContinuationInputText = goalcmd.ContinuationInputText
)

func newGoalTracker() *goalTracker {
	return goalcmd.NewTracker()
}

func goalTrackerForApp(a *App) *goalTracker {
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

func newGoalService(a *App) goalService {
	return goalcmd.NewService(goalAppAdapter{app: a})
}

func onThreadGoalUpdated(a *App, note codexrpc.ThreadGoalUpdatedNotification) {
	goalcmd.OnThreadGoalUpdated(goalAppAdapter{app: a}, note)
}

func onThreadGoalCleared(a *App, note codexrpc.ThreadGoalClearedNotification) {
	goalcmd.OnThreadGoalCleared(goalAppAdapter{app: a}, note)
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
	run func(goalService) (*callback.CardActionTriggerResponse, error),
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

type goalAppAdapter struct {
	app *App
}

func (a goalAppAdapter) State() goalcmd.StateProvider {
	if a.app == nil {
		return nil
	}
	return a.app.State()
}

func (a goalAppAdapter) Feishu() FeishuClient {
	if a.app == nil {
		return nil
	}
	return a.app.feishu
}

func (a goalAppAdapter) CodexClient() (goalcmd.CodexClient, error) {
	return requireCodexClient(a.app)
}

func (a goalAppAdapter) Tracker() *goalcmd.Tracker {
	return goalTrackerForApp(a.app)
}

func (a goalAppAdapter) MakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a.app, msg)
}

func (a goalAppAdapter) ReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a.app, chatType)
}

func (a goalAppAdapter) MenuCardBodyForSession(sessionKey, action, body string) string {
	return menuCardBodyForSession(a.app, sessionKey, action, body)
}

func (a goalAppAdapter) ActionStringValue(action *feishu.CardAction, key string) string {
	return actionStringValue(action, key)
}

func (a goalAppAdapter) ActionSessionKey(action *feishu.CardAction) string {
	return actionSessionKey(action)
}

func (a goalAppAdapter) CompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, fallbackAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a.app, action, sessionKey, rawCommand, fallbackAction)
}

func (a goalAppAdapter) DefaultWorkspaceID() string {
	return defaultWorkspaceID(a.app)
}

func (a goalAppAdapter) SessionBelongsToFrontend(sessionKey string) bool {
	return sessionBelongsToFrontend(a.app, sessionKey)
}

func (a goalAppAdapter) BindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	newRuntimeStateService(a.app).BindTurnSubmission(threadID, turnID, sessionKey, submissionID)
}

func (a goalAppAdapter) MarkTurnStartedAt(turnID string, startedAt time.Time) {
	newRuntimeStateService(a.app).MarkTurnStartedAt(turnID, startedAt)
}

func (a goalAppAdapter) RecordSubmissionSourceLinks(sub *state.Submission) {
	newReplyContinuationService(a.app).RecordSubmissionSourceLinks(sub)
}

func (a goalAppAdapter) RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string) {
	newReplyContinuationService(a.app).RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID)
}

func (a goalAppAdapter) NoteTurnStarted(sessionKey string, sub *state.Submission) {
	newTurnStreamService(a.app).NoteTurnStarted(sessionKey, sub)
}

func (a goalAppAdapter) MarkSessionThreadLive(sessionKey, threadID string) {
	markSessionThreadLive(a.app, sessionKey, threadID)
}
