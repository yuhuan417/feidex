package feishuapp

import (
	"context"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"log/slog"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func patchMaintenanceCard(a *App, messageID string, card map[string]any, warnMsg string, attrs ...any) {
	if a == nil {
		return
	}
	if strings.TrimSpace(messageID) == "" || card == nil {
		return
	}
	if err := newEffectRunner(a).Run(a.Context(), []application.Effect{application.PatchCard{Frontend: identity.FrontendID(a.FrontendID()), MessageID: messageID, View: feishuoutbound.Card(card)}}); err != nil {
		slog.Warn(warnMsg, append(attrs, "error", err)...)
	}
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
	return appmaintenance.StartRestartFromMessage(msg, sessionKey,
		func(ctx context.Context, parent string, card map[string]any, inThread bool) (string, error) {
			return replyCardWithIDEffect(ctx, a, parent, card, inThread)
		}, replyInThreadEnabled(a, msg.ChatType), begin, run, renderOperationCard, finishFailed)
}
