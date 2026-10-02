package json

import (
	"path/filepath"
	"testing"

	domainrouting "feidex/internal/domain/routing"
	"feidex/internal/state"
)

func TestGroupPrimaryRepositoryRoundTrip(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewGroupPrimaryRepository(store, "frontend-a")
	want := &domainrouting.GroupPrimaryState{FrontendID: "frontend-a", ChatType: "group", ChatID: "chat-a", Enabled: true}
	if err := repo.SaveGroupPrimaryState(want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetGroupPrimary("frontend-a", "group", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.FrontendID != want.FrontendID || got.ChatID != want.ChatID || !got.Enabled {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestGroupPrimaryRepositoryScopesFrontend(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewGroupPrimaryRepository(store, "frontend-a")
	if err := repo.SaveGroupPrimaryState(&domainrouting.GroupPrimaryState{FrontendID: "frontend-a", ChatType: "group", ChatID: "chat-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetGroupPrimary("frontend-b", "group", "chat-a"); err == nil || got != nil {
		t.Fatalf("cross-frontend lookup = %#v, err=%v", got, err)
	}
	if err := repo.SaveGroupPrimaryState(&domainrouting.GroupPrimaryState{FrontendID: "frontend-b", ChatType: "group", ChatID: "chat-a"}); err == nil {
		t.Fatal("cross-frontend write must be rejected")
	}
	if got := store.GetScopedGroupPrimary("frontend-b", GroupPrimaryID("frontend-b", "group", "chat-a")); got != nil {
		t.Fatalf("cross-frontend write persisted: %#v", got)
	}
}
