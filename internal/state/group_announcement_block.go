package state

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// GetGroupAnnouncementBlock returns an announcement block record by id without
// frontend filtering.
func (s *Store) GetGroupAnnouncementBlock(id string) *GroupAnnouncementBlock {
	return s.GetScopedGroupAnnouncementBlock("", id)
}

// GetScopedGroupAnnouncementBlock returns a record by id when it belongs to
// frontendID. An empty frontendID disables the scope check.
func (s *Store) GetScopedGroupAnnouncementBlock(frontendID, id string) *GroupAnnouncementBlock {
	frontendID = strings.TrimSpace(frontendID)
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.data.GroupAnnouncementBlocks[id]
	if !ok || record == nil {
		return nil
	}
	if frontendID != "" && strings.TrimSpace(record.FrontendID) != frontendID {
		return nil
	}
	return cloneGroupAnnouncementBlock(record)
}

// AllGroupAnnouncementBlocks returns deep copies of all persisted records.
func (s *Store) AllGroupAnnouncementBlocks() []*GroupAnnouncementBlock {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneGroupAnnouncementBlocks(s.data.GroupAnnouncementBlocks)
}

// GroupAnnouncementBlocksByChat returns records for one frontend and logical chat.
func (s *Store) GroupAnnouncementBlocksByChat(frontendID, chatType, chatID string) []*GroupAnnouncementBlock {
	frontendID = strings.TrimSpace(frontendID)
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if s == nil || chatID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneGroupAnnouncementBlocksMatching(s.data.GroupAnnouncementBlocks, func(record *GroupAnnouncementBlock) bool {
		if record == nil || strings.TrimSpace(record.FrontendID) != frontendID || strings.TrimSpace(record.ChatID) != chatID {
			return false
		}
		return chatType == "" || strings.ToLower(strings.TrimSpace(record.ChatType)) == chatType
	})
}

// UpsertGroupAnnouncementBlock persists a local frontend's announcement block record.
func (s *Store) UpsertGroupAnnouncementBlock(record *GroupAnnouncementBlock) error {
	if s == nil || record == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := cloneGroupAnnouncementBlock(record)
	if cp == nil || strings.TrimSpace(cp.ID) == "" {
		return nil
	}
	normalizeGroupAnnouncementBlockValues(cp)
	now := time.Now().Unix()
	if previous := s.data.GroupAnnouncementBlocks[cp.ID]; previous != nil && cp.CreatedAt == 0 {
		cp.CreatedAt = previous.CreatedAt
	}
	for id, previous := range s.data.GroupAnnouncementBlocks {
		if id == cp.ID || previous == nil {
			continue
		}
		if previous.FrontendID == cp.FrontendID && previous.ChatType == cp.ChatType && previous.ChatID == cp.ChatID {
			return fmt.Errorf("group announcement block already exists for frontend %q chat %q: %s", cp.FrontendID, cp.ChatID, id)
		}
	}
	if cp.CreatedAt == 0 {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	if s.data.GroupAnnouncementBlocks == nil {
		s.data.GroupAnnouncementBlocks = map[string]*GroupAnnouncementBlock{}
	}
	s.data.GroupAnnouncementBlocks[cp.ID] = cp
	return s.saveLocked()
}

// UpsertScopedGroupAnnouncementBlock persists a record owned by frontendID. A
// blank record frontend is filled from the scope; a different frontend is rejected.
func (s *Store) UpsertScopedGroupAnnouncementBlock(frontendID string, record *GroupAnnouncementBlock) error {
	if record == nil {
		return nil
	}
	frontendID = strings.TrimSpace(frontendID)
	cp := cloneGroupAnnouncementBlock(record)
	if cp == nil {
		return nil
	}
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = frontendID
	}
	if frontendID != "" && strings.TrimSpace(cp.FrontendID) != frontendID {
		return fmt.Errorf("group announcement block frontend %q does not match scope %q", cp.FrontendID, frontendID)
	}
	return s.UpsertGroupAnnouncementBlock(cp)
}

func cloneGroupAnnouncementBlock(record *GroupAnnouncementBlock) *GroupAnnouncementBlock {
	if record == nil {
		return nil
	}
	cp := *record
	normalizeGroupAnnouncementBlockValues(&cp)
	return &cp
}

func cloneGroupAnnouncementBlocks(src map[string]*GroupAnnouncementBlock) []*GroupAnnouncementBlock {
	return cloneGroupAnnouncementBlocksMatching(src, func(*GroupAnnouncementBlock) bool { return true })
}

func cloneGroupAnnouncementBlocksMatching(src map[string]*GroupAnnouncementBlock, match func(*GroupAnnouncementBlock) bool) []*GroupAnnouncementBlock {
	if len(src) == 0 {
		return nil
	}
	out := make([]*GroupAnnouncementBlock, 0, len(src))
	for _, record := range src {
		if record == nil || (match != nil && !match(record)) {
			continue
		}
		out = append(out, cloneGroupAnnouncementBlock(record))
	}
	slices.SortFunc(out, func(a, b *GroupAnnouncementBlock) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func normalizeGroupAnnouncementBlockValues(record *GroupAnnouncementBlock) bool {
	if record == nil {
		return false
	}
	before := *record
	record.ID = strings.TrimSpace(record.ID)
	record.FrontendID = strings.TrimSpace(record.FrontendID)
	record.ChatID = strings.TrimSpace(record.ChatID)
	record.ChatType = strings.ToLower(strings.TrimSpace(record.ChatType))
	record.BotOpenID = strings.TrimSpace(record.BotOpenID)
	record.BlockID = strings.TrimSpace(record.BlockID)
	record.Marker = strings.TrimSpace(record.Marker)
	record.LastContentHash = strings.TrimSpace(record.LastContentHash)
	if record.UpdatedAt == 0 && record.CreatedAt != 0 {
		record.UpdatedAt = record.CreatedAt
	}
	return before != *record
}

func normalizeGroupAnnouncementBlocks(src map[string]*GroupAnnouncementBlock) map[string]*GroupAnnouncementBlock {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]*GroupAnnouncementBlock, len(src))
	for key, record := range src {
		cp := cloneGroupAnnouncementBlock(record)
		if cp == nil {
			continue
		}
		if cp.ID == "" {
			cp.ID = strings.TrimSpace(key)
		}
		if cp.ID == "" {
			continue
		}
		dst[cp.ID] = cp
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func groupAnnouncementBlocksEqual(a, b map[string]*GroupAnnouncementBlock) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}
