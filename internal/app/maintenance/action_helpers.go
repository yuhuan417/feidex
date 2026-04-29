package maintenance

import (
	"context"
	"log/slog"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func PatchCard(feishuClient appcore.FeishuClient, messageID string, card map[string]any, warnMsg string, attrs ...any) {
	if feishuClient == nil || strings.TrimSpace(messageID) == "" || card == nil {
		return
	}
	if err := feishuClient.PatchCard(context.Background(), messageID, card); err != nil {
		attrs = append(attrs, "error", err)
		slog.Warn(warnMsg, attrs...)
	}
}

func FallbackResponse(
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
			Card:  &callback.Card{Type: "raw", Data: card},
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: cause.Error()},
		Card:  &callback.Card{Type: "raw", Data: failureCard(sessionKey, err.Error())},
	}, nil
}

func CompleteRestartRun[S any](
	action *feishu.CardAction,
	sessionKey string,
	begin func() (S, error),
	run func(messageID, sessionKey string),
	renderOperationCard func(sessionKey string, snapshot S) map[string]any,
	loadStatusCard func(context.Context) (map[string]any, error),
	failureCard func(sessionKey, errText string) map[string]any,
	toastText string,
) (*callback.CardActionTriggerResponse, error) {
	snapshot, err := begin()
	if err != nil {
		return FallbackResponse(sessionKey, err, loadStatusCard, failureCard)
	}
	messageID := ""
	if action != nil {
		messageID = strings.TrimSpace(action.MessageID)
	}
	go run(messageID, sessionKey)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  &callback.Card{Type: "raw", Data: renderOperationCard(sessionKey, snapshot)},
	}, nil
}

func StartRestartFromMessage[S any](
	msg *feishu.InboundMessage,
	sessionKey string,
	replyCard func(context.Context, string, map[string]any, bool) (string, error),
	replyInThread bool,
	begin func() (S, error),
	run func(messageID, sessionKey string),
	renderOperationCard func(sessionKey string, snapshot S) map[string]any,
	finishFailed func(message string),
) error {
	if msg == nil {
		return nil
	}
	snapshot, err := begin()
	if err != nil {
		return err
	}
	msgID, err := replyCard(context.Background(), msg.MessageID, renderOperationCard(sessionKey, snapshot), replyInThread)
	if err != nil {
		finishFailed("启动重启卡片失败: " + err.Error())
		return err
	}
	go run(msgID, sessionKey)
	return nil
}

func SnapshotLifecycle[S any](
	patchCard func(card map[string]any),
	sessionKey string,
	renderCard func(sessionKey string, snapshot S) map[string]any,
	updateState func(func(*S)) S,
	finishState func(result, message string) S,
	setProgress func(snapshot *S, phase, message string),
) (func(S), func(string, string) S, func(string, string)) {
	patch := func(snapshot S) {
		patchCard(renderCard(sessionKey, snapshot))
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
