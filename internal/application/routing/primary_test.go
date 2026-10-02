package routing

import (
	"testing"

	"feidex/internal/domain/identity"
	domainrouting "feidex/internal/domain/routing"
)

type memoryPrimaryRepository struct {
	state *domainrouting.GroupPrimaryState
}

func (r *memoryPrimaryRepository) GetGroupPrimary(_, _, _ string) (*domainrouting.GroupPrimaryState, error) {
	if r.state == nil {
		return nil, nil
	}
	cp := *r.state
	return &cp, nil
}

func (r *memoryPrimaryRepository) SaveGroupPrimaryState(value *domainrouting.GroupPrimaryState) error {
	cp := *value
	r.state = &cp
	return nil
}

func TestServiceSetPrimaryPersistsState(t *testing.T) {
	repo := &memoryPrimaryRepository{}
	got, err := (Service{Repository: repo}).SetPrimary(ChangePrimary{
		Frontend: "frontend-a", Chat: identity.ChatRef{Type: identity.ChatTypeGroup, ID: "chat-a"}, Enabled: true,
		Assignment: &domainrouting.AssignmentStamp{MessageID: "msg-a", CreatedAt: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Enabled || repo.state == nil || repo.state.LastAssignmentMessageID != "msg-a" {
		t.Fatalf("state = %#v, repo = %#v", got, repo.state)
	}
}

func TestServiceSetPrimaryIgnoresStaleAssignment(t *testing.T) {
	repo := &memoryPrimaryRepository{state: &domainrouting.GroupPrimaryState{
		FrontendID: "frontend-a", ChatType: "group", ChatID: "chat-a", Enabled: true,
		LastAssignmentMessageID: "msg-new", LastAssignmentCreatedAt: 20,
	}}
	got, err := (Service{Repository: repo}).SetPrimary(ChangePrimary{
		Frontend: "frontend-a", Chat: identity.ChatRef{Type: identity.ChatTypeGroup, ID: "chat-a"},
		Assignment: &domainrouting.AssignmentStamp{MessageID: "msg-old", CreatedAt: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Enabled || got.LastAssignmentMessageID != "msg-new" {
		t.Fatalf("stale assignment changed state: %#v", got)
	}
}
