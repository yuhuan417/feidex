// Package routing contains application use cases for frontend and group
// routing. It coordinates repositories but does not know about Feishu SDK or
// the persistent snapshot representation.
package routing

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/domain/identity"
	domainrouting "feidex/internal/domain/routing"
)

// PrimaryRepository is owned by this use case. The JSON-backed storage
// adapter implements it during migration.
type PrimaryRepository interface {
	GetGroupPrimary(frontendID, chatType, chatID string) (*domainrouting.GroupPrimaryState, error)
	SaveGroupPrimaryState(*domainrouting.GroupPrimaryState) error
}

type PrimaryInitializer interface {
	EnsureGroupPrimary(frontendID, chatType, chatID string, enabled bool) (*domainrouting.GroupPrimaryState, error)
}

// InitializationService owns the automatic primary decision. External facts
// such as bot membership are supplied through ports; Feishu handlers do not
// decide or persist primary state themselves.
type InitializationService struct {
	Repository    PrimaryRepository
	BotCount      func(context.Context, string) (int, error)
	LiveBotOpenID func() string
}

func (s InitializationService) Ensure(ctx context.Context, frontendID, chatType, chatID string) (*domainrouting.GroupPrimaryState, error) {
	if strings.TrimSpace(chatType) != "group" || strings.TrimSpace(chatID) == "" {
		return nil, nil
	}
	if s.Repository == nil || s.BotCount == nil {
		return nil, fmt.Errorf("primary initialization dependencies are unavailable")
	}
	if existing, err := s.Repository.GetGroupPrimary(frontendID, chatType, chatID); err != nil || existing != nil {
		return existing, err
	}
	count, err := s.BotCount(ctx, chatID)
	if err != nil {
		return nil, err
	}
	enabled := false
	if count == 1 {
		if s.LiveBotOpenID == nil {
			return nil, fmt.Errorf("bot open_id provider is unavailable")
		}
		if strings.TrimSpace(s.LiveBotOpenID()) == "" {
			return nil, fmt.Errorf("bot open_id is required to initialize group primary")
		}
		enabled = true
	}
	return (Service{Repository: s.Repository}).EnsurePrimary(frontendID, chatType, chatID, enabled)
}

// Service owns the group primary transition while delivery and transport stay
// outside the application layer.
type Service struct {
	Repository PrimaryRepository
}

// Lookup returns the persisted primary assignment for one frontend/chat
// scope. Read-side policy lives here so Feishu handlers only translate the
// result into their legacy presentation type.
func (s Service) Lookup(frontendID, chatType, chatID string) (*domainrouting.GroupPrimaryState, error) {
	chat := identity.ChatRef{Type: identity.ChatType(chatType), ID: chatID}.Normalize()
	frontend := string(identity.FrontendID(frontendID).Normalize())
	if string(chat.Type) != "group" || chat.ID == "" {
		return nil, fmt.Errorf("group primary scope is incomplete")
	}
	if s.Repository == nil {
		return nil, fmt.Errorf("group primary repository is nil")
	}
	return s.Repository.GetGroupPrimary(frontend, string(chat.Type), chat.ID)
}

// IsPrimary reports whether a persisted assignment enables this frontend for
// the chat. Missing state is intentionally false.
func (s Service) IsPrimary(frontendID, chatType, chatID string) (bool, error) {
	state, err := s.Lookup(frontendID, chatType, chatID)
	if err != nil || state == nil {
		return false, err
	}
	return state.Enabled, nil
}

// HasState reports whether an explicit assignment exists for the scope.
func (s Service) HasState(frontendID, chatType, chatID string) (bool, error) {
	state, err := s.Lookup(frontendID, chatType, chatID)
	return state != nil, err
}

// IsStaleAssignment applies the domain ordering rule to the persisted
// assignment marker. A missing record cannot be stale.
func (s Service) IsStaleAssignment(frontendID, chatType, chatID string, assignment domainrouting.AssignmentStamp) (bool, error) {
	state, err := s.Lookup(frontendID, chatType, chatID)
	if err != nil || state == nil {
		return false, err
	}
	return domainrouting.StaleAssignment(
		state.LastAssignmentMessageID,
		state.LastAssignmentCreatedAt,
		assignment.MessageID,
		assignment.CreatedAt,
	), nil
}

// EnsurePrimary creates the initial record only when storage has no explicit
// assignment. Existing operator choices always win over automatic discovery.
func (s Service) EnsurePrimary(frontendID, chatType, chatID string, enabled bool) (*domainrouting.GroupPrimaryState, error) {
	initializer, ok := s.Repository.(PrimaryInitializer)
	if !ok {
		return s.SetPrimary(ChangePrimary{Frontend: identity.FrontendID(frontendID), Chat: identity.ChatRef{Type: identity.ChatType(chatType), ID: chatID}, Enabled: enabled})
	}
	return initializer.EnsureGroupPrimary(frontendID, chatType, chatID, enabled)
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
