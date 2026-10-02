// Package json contains repository adapters for the current JSON-backed state
// store. It exposes domain repositories and keeps snapshot DTOs at the
// storage boundary.
package json

import (
	"fmt"
	"strings"

	domainrouting "feidex/internal/domain/routing"
	"feidex/internal/state"
)

// GroupPrimaryRepository adapts state.Store to the routing use case.
type GroupPrimaryRepository struct {
	store      *state.Store
	frontendID string
}

// NewGroupPrimaryRepository creates a frontend-scoped repository.
func NewGroupPrimaryRepository(store *state.Store, frontendID string) GroupPrimaryRepository {
	return GroupPrimaryRepository{store: store, frontendID: strings.TrimSpace(frontendID)}
}

func (r GroupPrimaryRepository) GetGroupPrimary(frontendID, chatType, chatID string) (*domainrouting.GroupPrimaryState, error) {
	if r.store == nil {
		return nil, fmt.Errorf("group primary store is not initialized")
	}
	if strings.TrimSpace(frontendID) != r.frontendID {
		return nil, fmt.Errorf("group primary frontend %q does not match scope %q", frontendID, r.frontendID)
	}
	primary := r.store.GetScopedGroupPrimary(r.frontendID, GroupPrimaryID(r.frontendID, chatType, chatID))
	if primary == nil {
		return nil, nil
	}
	return fromSnapshot(primary), nil
}

func (r GroupPrimaryRepository) SaveGroupPrimaryState(primary *domainrouting.GroupPrimaryState) error {
	if r.store == nil {
		return fmt.Errorf("group primary store is not initialized")
	}
	if primary == nil {
		return fmt.Errorf("group primary state is nil")
	}
	if strings.TrimSpace(primary.FrontendID) != r.frontendID {
		return fmt.Errorf("group primary frontend %q does not match scope %q", primary.FrontendID, r.frontendID)
	}
	return r.store.UpsertGroupPrimary(toSnapshot(primary))
}

func (r GroupPrimaryRepository) EnsureGroupPrimary(frontendID, chatType, chatID string, enabled bool) (*domainrouting.GroupPrimaryState, error) {
	if r.store == nil {
		return nil, fmt.Errorf("state store is nil")
	}
	if strings.TrimSpace(frontendID) != strings.TrimSpace(r.frontendID) {
		return nil, fmt.Errorf("frontend scope mismatch")
	}
	record := &state.GroupPrimary{ID: GroupPrimaryID(r.frontendID, chatType, chatID), FrontendID: r.frontendID, ChatType: chatType, ChatID: chatID, Enabled: enabled}
	result, err := r.store.EnsureGroupPrimary(record)
	if err != nil || result == nil {
		return nil, err
	}
	return &domainrouting.GroupPrimaryState{FrontendID: result.FrontendID, ChatType: result.ChatType, ChatID: result.ChatID, Enabled: result.Enabled, LastAssignmentMessageID: result.LastAssignmentMessageID, LastAssignmentCreatedAt: result.LastAssignmentCreatedAt}, nil
}

// GroupPrimaryID is the stable storage key for one frontend and group.
func GroupPrimaryID(frontendID, chatType, chatID string) string {
	return "primary_" + sanitizeIDPart(frontendID) + "_" + sanitizeIDPart(chatType) + "_" + sanitizeIDPart(chatID)
}

func sanitizeIDPart(value string) string {
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

func fromSnapshot(primary *state.GroupPrimary) *domainrouting.GroupPrimaryState {
	if primary == nil {
		return nil
	}
	return &domainrouting.GroupPrimaryState{
		FrontendID:              strings.TrimSpace(primary.FrontendID),
		ChatID:                  strings.TrimSpace(primary.ChatID),
		ChatType:                strings.TrimSpace(primary.ChatType),
		Enabled:                 primary.Enabled,
		LastAssignmentMessageID: strings.TrimSpace(primary.LastAssignmentMessageID),
		LastAssignmentCreatedAt: primary.LastAssignmentCreatedAt,
	}
}

func toSnapshot(primary *domainrouting.GroupPrimaryState) *state.GroupPrimary {
	if primary == nil {
		return nil
	}
	frontendID := strings.TrimSpace(primary.FrontendID)
	chatType := strings.ToLower(strings.TrimSpace(primary.ChatType))
	chatID := strings.TrimSpace(primary.ChatID)
	return &state.GroupPrimary{
		ID:                      GroupPrimaryID(frontendID, chatType, chatID),
		FrontendID:              frontendID,
		ChatID:                  chatID,
		ChatType:                chatType,
		Enabled:                 primary.Enabled,
		LastAssignmentMessageID: strings.TrimSpace(primary.LastAssignmentMessageID),
		LastAssignmentCreatedAt: primary.LastAssignmentCreatedAt,
	}
}
