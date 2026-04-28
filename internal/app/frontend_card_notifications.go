package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

const frontendCardNotificationKindFeishuPermissionIssue = "feishu_permission_issue"

func queueFrontendCardNotification(a *App, note state.FrontendCardNotification) {
	if a == nil || a.store == nil {
		return
	}
	if err := a.State().QueueFrontendCardNotification(note); err != nil {
		slog.Warn("queue frontend card notification failed",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"kind", strings.TrimSpace(note.Kind),
			"error", err,
		)
	}
}

func flushPendingFrontendCardNotifications(a *App, msg *feishu.InboundMessage) {
	if a == nil || a.feishu == nil || a.store == nil || msg == nil {
		return
	}
	notes, err := a.State().DrainFrontendCardNotifications()
	if err != nil {
		slog.Warn("load pending frontend card notifications failed",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"error", err,
		)
		return
	}
	if len(notes) == 0 {
		return
	}
	target := feishuNotifyTarget{
		ChatID: strings.TrimSpace(msg.ChatID),
		UserID: strings.TrimSpace(msg.UserID),
	}
	failed := make([]state.FrontendCardNotification, 0, len(notes))
	for _, note := range notes {
		if err := sendFrontendCardNotification(a, target, note); err != nil {
			slog.Warn("deliver pending frontend card notification failed",
				"frontend_id", strings.TrimSpace(a.frontendID),
				"chat_id", target.ChatID,
				"kind", strings.TrimSpace(note.Kind),
				"error", err,
			)
			failed = append(failed, note)
		}
	}
	for _, note := range failed {
		queueFrontendCardNotification(a, note)
	}
}

func sendFrontendCardNotification(a *App, target feishuNotifyTarget, note state.FrontendCardNotification) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sentMessageID, err := a.feishu.SendCard(ctx, chatID, a.feishu.SimpleStatusCard(title, color, body, nil))
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
