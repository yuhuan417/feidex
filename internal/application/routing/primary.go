// Package routing contains application use cases for frontend and group
// routing. It coordinates repositories but does not know about Feishu SDK or
// the persistent snapshot representation.
package routing

import (
	"fmt"

	"feidex/internal/domain/identity"
	domainrouting "feidex/internal/domain/routing"
)

// PrimaryRepository is owned by this use case. The JSON-backed storage
// adapter implements it during migration.
type PrimaryRepository interface {
	GetGroupPrimary(frontendID, chatType, chatID string) (*domainrouting.GroupPrimaryState, error)
	SaveGroupPrimaryState(*domainrouting.GroupPrimaryState) error
}

// Service owns the group primary transition while delivery and transport stay
// outside the application layer.
type Service struct {
	Repository PrimaryRepository
}

// ChangePrimary is a transport-independent input to the primary use case.
type ChangePrimary struct {
	Frontend   identity.FrontendID
	Chat       identity.ChatRef
	Enabled    bool
	Assignment *domainrouting.AssignmentStamp
}

// SetPrimary changes one frontend's primary state. A stale assignment is
// treated as an idempotent no-op and returns the stored state.
func (s Service) SetPrimary(input ChangePrimary) (*domainrouting.GroupPrimaryState, error) {
	frontendID := string(input.Frontend.Normalize())
	chat := input.Chat.Normalize()
	chatType, chatID := string(chat.Type), chat.ID
	// An empty frontend ID remains valid during migration for the existing
	// single-frontend legacy fallback. Multi-frontend callers always provide an
	// explicit ID through the adapter.
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("group primary scope is incomplete")
	}
	if s.Repository == nil {
		return nil, fmt.Errorf("group primary repository is nil")
	}
	current, err := s.Repository.GetGroupPrimary(frontendID, chatType, chatID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		current = &domainrouting.GroupPrimaryState{
			FrontendID: frontendID,
			ChatType:   chatType,
			ChatID:     chatID,
		}
	}
	next, changed := current.WithPrimary(input.Enabled, input.Assignment)
	if !changed {
		return current, nil
	}
	if err := s.Repository.SaveGroupPrimaryState(&next); err != nil {
		return nil, err
	}
	return &next, nil
}
