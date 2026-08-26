package appstate

import (
	"strings"

	"feidex/internal/state"
)

// GroupAnnouncementBlock returns this frontend's announcement block record for one group.
func (s *Store) GroupAnnouncementBlock(chatType, chatID string) *state.GroupAnnouncementBlock {
	if s == nil || s.Store == nil {
		return nil
	}
	records := s.GroupAnnouncementBlocksForChat(chatType, chatID)
	if len(records) == 0 {
		return nil
	}
	return records[0]
}

// GroupAnnouncementBlocksForChat returns this frontend's announcement block records for one chat.
func (s *Store) GroupAnnouncementBlocksForChat(chatType, chatID string) []*state.GroupAnnouncementBlock {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.GroupAnnouncementBlocksByChat(s.FrontendID, chatType, chatID)
}

// SaveGroupAnnouncementBlock persists an announcement block record in the current frontend scope.
func (s *Store) SaveGroupAnnouncementBlock(record *state.GroupAnnouncementBlock) error {
	if s == nil || s.Store == nil {
		return nil
	}
	if record != nil && strings.TrimSpace(record.ID) == "" {
		record = cloneGroupAnnouncementBlockForSave(record)
		record.ID = DefaultGroupAnnouncementBlockID(s.FrontendID, record.ChatType, record.ChatID)
	}
	return s.Store.UpsertScopedGroupAnnouncementBlock(s.FrontendID, record)
}

// DefaultGroupAnnouncementBlockID is the stable state key for one frontend in one group chat.
func DefaultGroupAnnouncementBlockID(frontendID, chatType, chatID string) string {
	return "announcement_" + sanitizeGroupPrimaryIDPart(frontendID) + "_" + sanitizeGroupPrimaryIDPart(chatType) + "_" + sanitizeGroupPrimaryIDPart(chatID)
}

func cloneGroupAnnouncementBlockForSave(record *state.GroupAnnouncementBlock) *state.GroupAnnouncementBlock {
	if record == nil {
		return nil
	}
	cp := *record
	return &cp
}
