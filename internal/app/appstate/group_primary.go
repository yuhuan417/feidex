package appstate

import (
	"strings"

	"feidex/internal/state"
)

// GroupPrimary returns this Feidex instance's primary owner setting for one group.
func (s *Store) GroupPrimary(chatType, chatID string) *state.GroupPrimary {
	if s == nil || s.Store == nil {
		return nil
	}
	primaries := s.GroupPrimariesForChat(chatType, chatID)
	if len(primaries) == 0 {
		return nil
	}
	return primaries[0]
}

// GroupPrimariesForChat returns this instance's primary records for one chat.
func (s *Store) GroupPrimariesForChat(chatType, chatID string) []*state.GroupPrimary {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.GroupPrimariesByChat("", chatType, chatID)
}

// SaveGroupPrimary persists primary owner state for this Feidex instance.
func (s *Store) SaveGroupPrimary(primary *state.GroupPrimary) error {
	if s == nil || s.Store == nil {
		return nil
	}
	if primary != nil && strings.TrimSpace(primary.ID) == "" {
		primary = cloneGroupPrimaryForSave(primary)
		primary.ID = DefaultGroupPrimaryID(s.FrontendID, primary.ChatType, primary.ChatID)
	}
	return s.Store.UpsertGroupPrimary(primary)
}

// DefaultGroupPrimaryID is the stable state key for one group chat.
func DefaultGroupPrimaryID(frontendID, chatType, chatID string) string {
	_ = frontendID
	return "primary_" + sanitizeGroupPrimaryIDPart(chatType) + "_" + sanitizeGroupPrimaryIDPart(chatID)
}

func cloneGroupPrimaryForSave(primary *state.GroupPrimary) *state.GroupPrimary {
	if primary == nil {
		return nil
	}
	cp := *primary
	return &cp
}

func sanitizeGroupPrimaryIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}
