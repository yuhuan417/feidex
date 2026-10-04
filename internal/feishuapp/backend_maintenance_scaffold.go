package feishuapp

import (
	"context"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	frontendruntime "feidex/internal/runtime"
	"log/slog"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func patchMaintenanceCard(ctx context.Context, frontendID string, runner frontendruntime.EffectRunner, messageID string, card map[string]any, warnMsg string, attrs ...any) {
	if strings.TrimSpace(messageID) == "" || card == nil {
		return
	}
	if err := runner.Run(ctx, []application.Effect{application.PatchCard{Frontend: identity.FrontendID(frontendID), MessageID: messageID, View: feishuoutbound.Card(card)}}); err != nil {
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
	view frontendConfigView,
	msg *feishu.InboundMessage,
	reply func(context.Context, string, map[string]any, bool) (string, error),
	inThread bool,
	begin func() (S, error),
	run func(messageID, sessionKey string),
	renderOperationCard func(sessionKey string, snapshot S) map[string]any,
	finishFailed func(message string),
) error {
	sessionKey := view.makeSessionKey(msg)
	return appmaintenance.StartRestartFromMessage(msg, sessionKey,
		reply, inThread, begin, run, renderOperationCard, finishFailed)
}
