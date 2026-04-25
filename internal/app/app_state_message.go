package app

import (
	"strings"

	"feidex/internal/state"
)

func (s *appStateFacade) nextLocalID(prefix string) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.NextLocalID(strings.TrimSpace(prefix))
}

func (s *appStateFacade) messageLink(messageID string) *state.MessageLink {
	if s == nil || s.store == nil {
		return nil
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	if link := s.store.GetScopedMessageLink(s.frontendID, messageID); link != nil {
		return link
	}
	if s.legacyFallback && s.frontendID != "" {
		return s.store.GetMessageLink(messageID)
	}
	return nil
}

func (s *appStateFacade) queueFrontendCardNotification(note state.FrontendCardNotification) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.AppendFrontendCardNotification(strings.TrimSpace(s.frontendID), note)
}

func (s *appStateFacade) frontendCardNotifications() []state.FrontendCardNotification {
	if s == nil || s.store == nil {
		return nil
	}
	notes := s.store.FrontendCardNotifications(strings.TrimSpace(s.frontendID))
	if len(notes) == 0 && s.legacyFallback && strings.TrimSpace(s.frontendID) != "" {
		return s.store.FrontendCardNotifications("")
	}
	return notes
}

func (s *appStateFacade) drainFrontendCardNotifications() ([]state.FrontendCardNotification, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	notes, err := s.store.DrainFrontendCardNotifications(strings.TrimSpace(s.frontendID))
	if err != nil || len(notes) > 0 || !s.legacyFallback || strings.TrimSpace(s.frontendID) == "" {
		return notes, err
	}
	return s.store.DrainFrontendCardNotifications("")
}

func (s *appStateFacade) saveMessageLink(link *state.MessageLink) error {
	if s == nil || s.store == nil || link == nil {
		return nil
	}
	cp := *link
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.frontendID
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.backend
	}
	return s.store.UpsertMessageLink(&cp)
}

func (s *appStateFacade) deleteMessageLinks(match func(*state.MessageLink) bool) {
	if s == nil || s.store == nil || match == nil {
		return
	}
	s.store.DeleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil || !s.matchesFrontend(link.FrontendID) {
			return false
		}
		return match(link)
	})
}
