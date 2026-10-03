package appstate

import (
	"strings"

	"feidex/internal/state"
)

// NextLocalID allocates the next local id for a prefix.
func (s *Store) NextLocalID(prefix string) (string, error) {
	if s == nil || s.stateStore() == nil {
		return "", nil
	}
	return s.stateStore().NextLocalID(strings.TrimSpace(prefix))
}

// MessageLink returns a frontend-scoped message link by message id.
func (s *Store) MessageLink(messageID string) *state.MessageLink {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	return s.stateStore().GetScopedMessageLink(s.scopeFrontendID(), messageID)
}

// SaveMessageLink persists a message link scoped to the current frontend.
func (s *Store) SaveMessageLink(link *state.MessageLink) error {
	if s == nil || s.stateStore() == nil || link == nil {
		return nil
	}
	cp := *link
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.scopeFrontendID()
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.scopeBackend()
	}
	return s.stateStore().UpsertMessageLink(&cp)
}

// DeleteMessageLinks deletes frontend-scoped message links matching fn.
func (s *Store) DeleteMessageLinks(match func(*state.MessageLink) bool) {
	if s == nil || s.stateStore() == nil || match == nil {
		return
	}
	s.stateStore().DeleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil || !s.matchesFrontend(link.FrontendID) {
			return false
		}
		return match(link)
	})
}

// QueueFrontendCardNotification appends a frontend-scoped card notification.
func (s *Store) QueueFrontendCardNotification(note state.FrontendCardNotification) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().AppendFrontendCardNotification(strings.TrimSpace(s.scopeFrontendID()), note)
}

// DeleteFrontendCardNotificationsByCollapseKey drops queued notifications of
// one collapse key for this frontend.
func (s *Store) DeleteFrontendCardNotificationsByCollapseKey(collapseKey string) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().DeleteFrontendCardNotificationsByCollapseKey(strings.TrimSpace(s.scopeFrontendID()), collapseKey)
}

// FrontendCardNotifications returns pending frontend-scoped card notifications.
func (s *Store) FrontendCardNotifications() []state.FrontendCardNotification {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().FrontendCardNotifications(s.scopeFrontendID())
}

// DrainFrontendCardNotifications drains pending frontend-scoped notifications.
func (s *Store) DrainFrontendCardNotifications() ([]state.FrontendCardNotification, error) {
	if s == nil || s.stateStore() == nil {
		return nil, nil
	}
	return s.stateStore().DrainFrontendCardNotifications(s.scopeFrontendID())
}
