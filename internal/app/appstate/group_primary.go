package appstate

import (
	"strings"

	"feidex/internal/state"
)

// GroupPrimary returns this frontend's local primary setting for one group.
func (s *Store) GroupPrimary(chatType, chatID string) *state.GroupPrimary {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().GetScopedGroupPrimary(s.scopeFrontendID(), DefaultGroupPrimaryID(s.scopeFrontendID(), chatType, chatID))
}

// GroupPrimariesForChat returns this frontend's primary records for one chat.
func (s *Store) GroupPrimariesForChat(chatType, chatID string) []*state.GroupPrimary {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	primary := s.GroupPrimary(chatType, chatID)
	if primary == nil {
		return nil
	}
	return []*state.GroupPrimary{primary}
}

// SaveGroupPrimary persists primary state for the current frontend.
func (s *Store) SaveGroupPrimary(primary *state.GroupPrimary) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().UpsertGroupPrimary(s.groupPrimaryForSave(primary))
}

// EnsureGroupPrimary initializes this frontend's primary state without
// overwriting an explicit primary command.
func (s *Store) EnsureGroupPrimary(primary *state.GroupPrimary) (*state.GroupPrimary, error) {
	if s == nil || s.stateStore() == nil {
		return nil, nil
	}
	return s.stateStore().EnsureGroupPrimary(s.groupPrimaryForSave(primary))
}

// DefaultGroupPrimaryID is the stable state key for one frontend in one group.
func DefaultGroupPrimaryID(frontendID, chatType, chatID string) string {
	return "primary_" + sanitizeGroupPrimaryIDPart(frontendID) + "_" + sanitizeGroupPrimaryIDPart(chatType) + "_" + sanitizeGroupPrimaryIDPart(chatID)
}

func (s *Store) groupPrimaryForSave(primary *state.GroupPrimary) *state.GroupPrimary {
	if primary == nil {
		return nil
	}
	cp := *primary
	cp.FrontendID = strings.TrimSpace(s.scopeFrontendID())
	if strings.TrimSpace(cp.ChatType) != "" && strings.TrimSpace(cp.ChatID) != "" {
		cp.ID = DefaultGroupPrimaryID(s.scopeFrontendID(), cp.ChatType, cp.ChatID)
	}
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
