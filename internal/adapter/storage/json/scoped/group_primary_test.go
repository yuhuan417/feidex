package appstate

import (
	"testing"

	"feidex/internal/state"
)

func TestGroupPrimaryUsesCanonicalKeyOnly(t *testing.T) {
	store := newTestStateStore(t)
	frontend := NewScoped(store, "default", "", false)
	if err := store.UpsertGroupPrimary(&state.GroupPrimary{
		ID:         "primary_default_group_chat-1",
		FrontendID: "legacy",
		ChatID:     "chat-1",
		ChatType:   "group",
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertGroupPrimary(legacy) error = %v", err)
	}

	if got := frontend.GroupPrimary("group", "chat-1"); got != nil {
		t.Fatalf("GroupPrimary() returned legacy frontend-scoped record: %+v", got)
	}
}

func TestGroupPrimaryUsesFrontendScopedKey(t *testing.T) {
	store := newTestStateStore(t)
	frontendA := NewScoped(store, "frontend-a", "", false)
	frontendB := NewScoped(store, "frontend-b", "", false)

	if err := frontendA.SaveGroupPrimary(&state.GroupPrimary{ChatType: "group", ChatID: "chat-1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := frontendB.SaveGroupPrimary(&state.GroupPrimary{ChatType: "group", ChatID: "chat-1", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if got := frontendA.GroupPrimary("group", "chat-1"); got == nil || !got.Enabled || got.FrontendID != "frontend-a" {
		t.Fatalf("frontend-a primary = %+v", got)
	}
	if got := frontendB.GroupPrimary("group", "chat-1"); got == nil || got.Enabled || got.FrontendID != "frontend-b" {
		t.Fatalf("frontend-b primary = %+v", got)
	}
}

func TestGroupPrimarySaveAlwaysUsesCurrentFrontendScope(t *testing.T) {
	store := newTestStateStore(t)
	frontend := NewScoped(store, "frontend-a", "", false)

	if err := frontend.SaveGroupPrimary(&state.GroupPrimary{
		ID:         "primary-foreign",
		FrontendID: "frontend-b",
		ChatType:   "group",
		ChatID:     "chat-1",
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := frontend.GroupPrimary("group", "chat-1"); got == nil || got.FrontendID != "frontend-a" || !got.Enabled {
		t.Fatalf("current frontend primary = %+v", got)
	}
	if got := store.GetScopedGroupPrimary("frontend-b", "primary-foreign"); got != nil {
		t.Fatalf("foreign frontend primary was persisted: %+v", got)
	}
}
