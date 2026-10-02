package routing

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/domain/identity"
	domainrouting "feidex/internal/domain/routing"
)

type memoryPrimaryRepository struct {
	state *domainrouting.GroupPrimaryState
	saves int
}

func (r *memoryPrimaryRepository) GetGroupPrimary(_, _, _ string) (*domainrouting.GroupPrimaryState, error) {
	if r.state == nil {
		return nil, nil
	}
	cp := *r.state
	return &cp, nil
}

func (r *memoryPrimaryRepository) SaveGroupPrimaryState(value *domainrouting.GroupPrimaryState) error {
	r.saves++
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

func TestInitializationServicePreservesExistingAssignment(t *testing.T) {
	repo := &memoryPrimaryRepository{state: &domainrouting.GroupPrimaryState{
		FrontendID: "frontend-1", ChatType: "group", ChatID: "chat-1", Enabled: false,
	}}
	botCountCalls := 0
	result, err := (InitializationService{
		Repository: repo,
		BotCount: func(context.Context, string) (int, error) {
			botCountCalls++
			return 1, nil
		},
		LiveBotOpenID: func() string { return "ou_live" },
	}).Ensure(context.Background(), "frontend-1", "group", "chat-1")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result == nil || result.Enabled {
		t.Fatalf("Ensure() result = %#v, want existing disabled state", result)
	}
	if botCountCalls != 0 || repo.saves != 0 {
		t.Fatalf("existing assignment triggered discovery: botCountCalls=%d saves=%d", botCountCalls, repo.saves)
	}
}

func TestInitializationServiceEnablesSingleBot(t *testing.T) {
	repo := &memoryPrimaryRepository{}
	result, err := (InitializationService{
		Repository: repo, BotCount: func(context.Context, string) (int, error) { return 1, nil },
		LiveBotOpenID: func() string { return "ou_live" },
	}).Ensure(context.Background(), "frontend-1", "group", "chat-1")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result == nil || !result.Enabled || repo.saves != 1 {
		t.Fatalf("result = %#v, saves = %d, want enabled and one save", result, repo.saves)
	}
}

func TestInitializationServiceDisablesMultipleBots(t *testing.T) {
	repo := &memoryPrimaryRepository{}
	result, err := (InitializationService{
		Repository: repo, BotCount: func(context.Context, string) (int, error) { return 2, nil },
		LiveBotOpenID: func() string { return "ou_live" },
	}).Ensure(context.Background(), "frontend-1", "group", "chat-1")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if result == nil || result.Enabled || repo.saves != 1 {
		t.Fatalf("result = %#v, saves = %d, want disabled and one save", result, repo.saves)
	}
}

func TestInitializationServiceRequiresLiveBotForSingleBot(t *testing.T) {
	for name, openID := range map[string]string{"empty": "", "provider missing": "missing"} {
		t.Run(name, func(t *testing.T) {
			repo := &memoryPrimaryRepository{}
			service := InitializationService{Repository: repo, BotCount: func(context.Context, string) (int, error) { return 1, nil }}
			if openID != "missing" {
				service.LiveBotOpenID = func() string { return openID }
			}
			if _, err := service.Ensure(context.Background(), "frontend-1", "group", "chat-1"); err == nil {
				t.Fatal("Ensure() error = nil, want missing open_id error")
			}
			if repo.saves != 0 {
				t.Fatalf("saves = %d, want 0", repo.saves)
			}
		})
	}
}

func TestInitializationServicePropagatesBotCountError(t *testing.T) {
	wantErr := errors.New("lookup failed")
	_, err := (InitializationService{
		Repository: &memoryPrimaryRepository{}, BotCount: func(context.Context, string) (int, error) { return 0, wantErr },
		LiveBotOpenID: func() string { return "ou_live" },
	}).Ensure(context.Background(), "frontend-1", "group", "chat-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Ensure() error = %v, want %v", err, wantErr)
	}
}
