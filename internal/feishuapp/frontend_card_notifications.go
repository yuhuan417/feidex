package feishuapp

import (
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	frontendapp "feidex/internal/application/frontend"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	"feidex/internal/runtime"

	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func queueFrontendCardNotification(a *App, note state.FrontendCardNotification) {
	if a == nil || a.store == nil {
		return
	}
	a.bindings.Notifications.Queue(note)
}

type notificationCardClient interface {
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
	UrgentApp(context.Context, string, string) error
}

type notificationSender struct {
	frontend identity.FrontendID
	client   notificationCardClient
	runner   runtime.EffectRunner
}

func NotificationSender(client notificationCardClient, frontend string, runner runtime.EffectRunner) frontendapp.NotificationSender {
	return notificationSender{frontend: identity.FrontendID(frontend), client: client, runner: runner}
}

func (p notificationSender) DeliverNotification(ctx context.Context, chatID, userID string, note routing.FrontendCardNotification) error {
	if p.client == nil {
		return nil
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	title := strings.TrimSpace(note.Title)
	body := strings.TrimSpace(note.Body)
	if title == "" || body == "" {
		return nil
	}
	color := strings.TrimSpace(note.Color)
	if color == "" {
		color = "blue"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	card := p.client.SimpleStatusCard(title, color, body, nil)
	sentMessageID, err := p.runner.RunSendCard(ctx, application.SendCard{
		Frontend: p.frontend, Chat: identity.ChatRef{ID: chatID}, View: feishuoutbound.Card(card),
	})
	if err != nil {
		return err
	}
	if userID := strings.TrimSpace(userID); userID != "" && strings.TrimSpace(sentMessageID) != "" {
		if urgentErr := p.client.UrgentApp(ctx, sentMessageID, userID); urgentErr != nil {
			slog.Warn("frontend card notification urgent_app failed",
				"message_id", sentMessageID,
				"user_id", userID,
				"error", urgentErr,
			)
		}
	}
	return nil
}
