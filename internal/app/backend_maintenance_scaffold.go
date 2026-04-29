package app

import (
	"context"
	appmaintenance "feidex/internal/app/maintenance"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func patchMaintenanceCard(a *App, messageID string, card map[string]any, warnMsg string, attrs ...any) {
	if a == nil {
		return
	}
	appmaintenance.PatchCard(a.feishu, messageID, card, warnMsg, attrs...)
}

func completeMaintenanceAsyncAction(a *App,
	action *feishu.CardAction,
	rawCommand string,
	toastText string,
	preparingCard func(sessionKey string) map[string]any,
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	return completeAsyncCommandAction(a,
		action,
		sessionKey,
		rawCommand,
		"menu.group.system",
		toastText,
		preparingCard(sessionKey),
		nil,
		failureCard,
		patchWarnMsg,
	)
}

func maintenanceFallbackResponse(
	sessionKey string,
	cause error,
	loadStatusCard func(context.Context) (map[string]any, error),
	failureCard func(sessionKey, errText string) map[string]any,
) (*callback.CardActionTriggerResponse, error) {
	return appmaintenance.FallbackResponse(sessionKey, cause, loadStatusCard, failureCard)
}

func completeMaintenanceRestartRun[S any](
	a *App,
	action *feishu.CardAction,
	begin func() (S, error),
	run func(messageID, sessionKey string),
	renderOperationCard func(sessionKey string, snapshot S) map[string]any,
	loadStatusCard func(context.Context) (map[string]any, error),
	failureCard func(sessionKey, errText string) map[string]any,
	toastText string,
) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	return appmaintenance.CompleteRestartRun(action, sessionKey, begin, run, renderOperationCard, loadStatusCard, failureCard, toastText)
}

func startMaintenanceRestartFromMessage[S any](
	a *App,
	msg *feishu.InboundMessage,
	begin func() (S, error),
	run func(messageID, sessionKey string),
	renderOperationCard func(sessionKey string, snapshot S) map[string]any,
	finishFailed func(message string),
) error {
	sessionKey := makeSessionKey(a, msg)
	return appmaintenance.StartRestartFromMessage(msg, sessionKey, a.feishu.ReplyCard, replyInThreadEnabled(a, msg.ChatType), begin, run, renderOperationCard, finishFailed)
}

func maintenanceSnapshotLifecycle[S any](
	a *App,
	messageID string,
	sessionKey string,
	warnMsg string,
	renderCard func(sessionKey string, snapshot S) map[string]any,
	updateState func(func(*S)) S,
	finishState func(result, message string) S,
	setProgress func(snapshot *S, phase, message string),
) (func(S), func(string, string) S, func(string, string)) {
	return appmaintenance.SnapshotLifecycle(
		func(card map[string]any) {
			patchMaintenanceCard(a, messageID, card, warnMsg, "message_id", messageID)
		},
		sessionKey,
		renderCard,
		updateState,
		finishState,
		setProgress,
	)
}
