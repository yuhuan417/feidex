package state

import (
	"path/filepath"
	"testing"
)

func TestGroupPrimaryPersistScopeAndClone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.UpsertScopedGroupPrimary("frontend-a", &GroupPrimary{
		ID:       " primary-a ",
		ChatID:   " chat-1 ",
		ChatType: " GROUP ",
		Primary:  true,
	}); err != nil {
		t.Fatalf("UpsertScopedGroupPrimary(frontend-a) error = %v", err)
	}
	gotA := store.GetScopedGroupPrimary("frontend-a", "primary-a")
	if gotA == nil || gotA.FrontendID != "frontend-a" || gotA.ChatID != "chat-1" || gotA.ChatType != "group" || !gotA.Primary {
		t.Fatalf("frontend-a group primary = %+v", gotA)
	}
	gotA.Primary = false
	if again := store.GetScopedGroupPrimary("frontend-a", "primary-a"); again == nil || !again.Primary {
		t.Fatalf("GetScopedGroupPrimary returned shared state: %+v", again)
	}

	if err := store.UpsertScopedGroupPrimary("frontend-b", &GroupPrimary{
		ID:       "primary-b",
		ChatID:   "chat-1",
		ChatType: "group",
		Primary:  true,
	}); err != nil {
		t.Fatalf("UpsertScopedGroupPrimary() error = %v", err)
	}
	if got := store.GetScopedGroupPrimary("frontend-a", "primary-b"); got != nil {
		t.Fatalf("cross-frontend group primary lookup = %+v, want nil", got)
	}
	if got := store.GetScopedGroupPrimary("frontend-b", "primary-b"); got == nil || got.FrontendID != "frontend-b" || !got.Primary {
		t.Fatalf("frontend-b group primary = %+v", got)
	}
}
