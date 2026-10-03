package frontend

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/domain/routing"
)

type NotificationRepository interface {
	QueueFrontendCardNotification(routing.FrontendCardNotification) error
	DrainFrontendCardNotifications() ([]routing.FrontendCardNotification, error)
	DeleteFrontendCardNotificationsByCollapseKey(string) error
}
type NotificationSender interface {
	DeliverNotification(context.Context, string, string, routing.FrontendCardNotification) error
}
type Notifications struct {
	Repository NotificationRepository
	Sender     NotificationSender
	Context    func() context.Context
}

func (s Notifications) Queue(note routing.FrontendCardNotification) {
	if strings.TrimSpace(note.Title) == "" || strings.TrimSpace(note.Body) == "" {
		return
	}
	if err := s.Repository.QueueFrontendCardNotification(note); err != nil {
		slog.Warn("queue frontend notification failed", "kind", note.Kind, "error", err)
	}
}

func (s Notifications) Clear(key string) error {
	return s.Repository.DeleteFrontendCardNotificationsByCollapseKey(key)
}

func (s Notifications) Flush(chatID, userID string) {
	if strings.TrimSpace(chatID) == "" {
		return
	}
	notes, err := s.Repository.DrainFrontendCardNotifications()
	if err != nil {
		slog.Warn("load pending frontend notifications failed", "error", err)
		return
	}
	for _, note := range notes {
		if err := s.Sender.DeliverNotification(s.Context(), chatID, userID, note); err != nil {
			slog.Warn("deliver pending frontend notification failed", "kind", note.Kind, "error", err)
			s.Queue(note)
		}
	}
}
