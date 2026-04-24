package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func patchMaintenanceCard(a *App, messageID string, card map[string]any, warnMsg string, attrs ...any) {
	if a == nil || a.feishu == nil || strings.TrimSpace(messageID) == "" || card == nil {
		return
	}
	if err := a.feishu.PatchCard(context.Background(), messageID, card); err != nil {
		attrs = append(attrs, "error", err)
		slog.Warn(warnMsg, attrs...)
	}
}

func (a *App) completeMaintenanceAsyncAction(
	action *feishu.CardAction,
	rawCommand string,
	toastText string,
	preparingCard func(sessionKey string) map[string]any,
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	return a.completeAsyncCommandAction(
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	card, err := loadStatusCard(ctx)
	if err == nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: cause.Error()},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: cause.Error()},
		Card:  rawCard(failureCard(sessionKey, err.Error())),
	}, nil
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
	snapshot, err := begin()
	if err != nil {
		return maintenanceFallbackResponse(sessionKey, err, loadStatusCard, failureCard)
	}
	messageID := strings.TrimSpace(action.MessageID)
	go run(messageID, sessionKey)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(renderOperationCard(sessionKey, snapshot)),
	}, nil
}

func startMaintenanceRestartFromMessage[S any](
	a *App,
	msg *feishu.InboundMessage,
	begin func() (S, error),
	run func(messageID, sessionKey string),
	renderOperationCard func(sessionKey string, snapshot S) map[string]any,
	finishFailed func(message string),
) error {
	if msg == nil {
		return nil
	}
	sessionKey := makeSessionKey(a, msg)
	snapshot, err := begin()
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, renderOperationCard(sessionKey, snapshot), replyInThreadEnabled(a, msg.ChatType))
	if err != nil {
		finishFailed("启动重启卡片失败: " + err.Error())
		return err
	}
	go run(msgID, sessionKey)
	return nil
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
	patch := func(snapshot S) {
		patchMaintenanceCard(a, messageID, renderCard(sessionKey, snapshot), warnMsg,
			"message_id", messageID,
		)
	}
	update := func(phase, message string) S {
		snapshot := updateState(func(snapshot *S) {
			setProgress(snapshot, phase, message)
		})
		patch(snapshot)
		return snapshot
	}
	finalize := func(result, message string) {
		patch(finishState(result, message))
	}
	return patch, update, finalize
}
