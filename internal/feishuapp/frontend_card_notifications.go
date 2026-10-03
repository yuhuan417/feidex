package feishuapp

import (
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	frontendapp "feidex/internal/application/frontend"
	"feidex/internal/domain/routing"

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

func flushPendingFrontendCardNotifications(a *App, msg *feishu.InboundMessage) {
	if a == nil || a.feishu == nil || a.store == nil || msg == nil {
		return
	}
	a.bindings.Notifications.Flush(msg.ChatID, msg.UserID)
}

type notificationSender struct{ app *App }

func NotificationSender(a *App) frontendapp.NotificationSender { return notificationSender{app: a} }
func (p notificationSender) DeliverNotification(_ context.Context, chatID, userID string, note routing.FrontendCardNotification) error {
	return sendFrontendCardNotification(p.app, appfeishuwrap.NotifyTarget{ChatID: chatID, UserID: userID}, note)
}

func sendFrontendCardNotification(a *App, target appfeishuwrap.NotifyTarget, note state.FrontendCardNotification) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	chatID := strings.TrimSpace(target.ChatID)
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
	ctx, cancel := context.WithTimeout(a.Context(), 5*time.Second)
	defer cancel()
	sentMessageID, err := sendCardWithIDEffect(ctx, a, chatID, a.feishu.SimpleStatusCard(title, color, body, nil))
	if err != nil {
		return err
	}
	if userID := strings.TrimSpace(target.UserID); userID != "" && strings.TrimSpace(sentMessageID) != "" {
		if urgentErr := a.feishu.UrgentApp(ctx, sentMessageID, userID); urgentErr != nil {
			slog.Warn("frontend card notification urgent_app failed",
				"message_id", sentMessageID,
				"user_id", userID,
				"error", urgentErr,
			)
		}
	}
	return nil
}
