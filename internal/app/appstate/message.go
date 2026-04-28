package appstate

import (
	"strings"

	"feidex/internal/state"
)

// NextLocalID allocates the next local id for a prefix.
func (s *Store) NextLocalID(prefix string) (string, error) {
	if s == nil || s.Store == nil {
		return "", nil
	}
	return s.Store.NextLocalID(strings.TrimSpace(prefix))
}

// MessageLink returns a frontend-scoped message link by message id.
func (s *Store) MessageLink(messageID string) *state.MessageLink {
	if s == nil || s.Store == nil {
		return nil
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	if link := s.Store.GetScopedMessageLink(s.FrontendID, messageID); link != nil {
		return link
	}
	if s.LegacyFallback && s.FrontendID != "" {
		return s.Store.GetMessageLink(messageID)
	}
	return nil
}

// SaveMessageLink persists a message link scoped to the current frontend.
func (s *Store) SaveMessageLink(link *state.MessageLink) error {
	if s == nil || s.Store == nil || link == nil {
		return nil
	}
	cp := *link
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.FrontendID
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.Backend
	}
	return s.Store.UpsertMessageLink(&cp)
}

// DeleteMessageLinks deletes frontend-scoped message links matching fn.
func (s *Store) DeleteMessageLinks(match func(*state.MessageLink) bool) {
	if s == nil || s.Store == nil || match == nil {
		return
	}
	s.Store.DeleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil || !s.MatchesFrontend(link.FrontendID) {
			return false
		}
		return match(link)
	})
}

// QueueFrontendCardNotification appends a frontend-scoped card notification.
func (s *Store) QueueFrontendCardNotification(note state.FrontendCardNotification) error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.AppendFrontendCardNotification(strings.TrimSpace(s.FrontendID), note)
}

// FrontendCardNotifications returns pending frontend-scoped card notifications.
func (s *Store) FrontendCardNotifications() []state.FrontendCardNotification {
	if s == nil || s.Store == nil {
		return nil
	}
	notes := s.Store.FrontendCardNotifications(strings.TrimSpace(s.FrontendID))
	if len(notes) == 0 && s.LegacyFallback && strings.TrimSpace(s.FrontendID) != "" {
		return s.Store.FrontendCardNotifications("")
	}
	return notes
}

// DrainFrontendCardNotifications drains pending frontend-scoped notifications.
func (s *Store) DrainFrontendCardNotifications() ([]state.FrontendCardNotification, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	notes, err := s.Store.DrainFrontendCardNotifications(strings.TrimSpace(s.FrontendID))
	if err != nil || len(notes) > 0 || !s.LegacyFallback || strings.TrimSpace(s.FrontendID) == "" {
		return notes, err
	}
	return s.Store.DrainFrontendCardNotifications("")
}
