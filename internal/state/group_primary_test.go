package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGroupPrimaryPersistScopeAndMigrateBindingPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := `{
  "version": 9,
  "sessions": {},
  "agent_bindings": {
    "binding-1": {
      "id": "binding-1",
      "frontend_id": "frontend-a",
      "chat_id": "chat-1",
      "chat_type": "group",
      "primary": true,
      "status": "active",
      "created_at": 10,
      "updated_at": 20
    }
  }
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	primaries := store.GroupPrimariesByChat("frontend-a", "group", "chat-1")
	if len(primaries) != 1 || !primaries[0].Primary || primaries[0].CreatedAt != 10 || primaries[0].UpdatedAt != 20 {
		t.Fatalf("migrated primaries = %+v", primaries)
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
