package state

import (
	"path/filepath"
	"testing"
)

func TestGroupAnnouncementBlockPersistScopeAndClone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	record := &GroupAnnouncementBlock{
		ID:              " announcement-a ",
		FrontendID:      " frontend-a ",
		ChatID:          " chat-1 ",
		ChatType:        " GROUP ",
		BotOpenID:       " bot-a ",
		BlockID:         " block-a ",
		Marker:          " marker-a ",
		LastContentHash: " hash-a ",
	}
	if err := store.UpsertGroupAnnouncementBlock(record); err != nil {
		t.Fatalf("UpsertGroupAnnouncementBlock() error = %v", err)
	}

	saved := store.GetScopedGroupAnnouncementBlock("frontend-a", "announcement-a")
	if saved == nil || saved.ChatType != "group" || saved.ChatID != "chat-1" || saved.BotOpenID != "bot-a" || saved.BlockID != "block-a" || saved.Marker != "marker-a" || saved.LastContentHash != "hash-a" {
		t.Fatalf("saved announcement block = %+v", saved)
	}
	if saved.CreatedAt == 0 || saved.UpdatedAt == 0 {
		t.Fatalf("announcement block timestamps = %+v", saved)
	}
	if got := store.GetScopedGroupAnnouncementBlock("frontend-b", "announcement-a"); got != nil {
		t.Fatalf("cross-frontend lookup = %+v, want nil", got)
	}

	saved.BlockID = "mutated"
	if again := store.GetGroupAnnouncementBlock("announcement-a"); again == nil || again.BlockID != "block-a" {
		t.Fatalf("GetGroupAnnouncementBlock returned shared state: %+v", again)
	}

	if err := store.UpsertGroupAnnouncementBlock(&GroupAnnouncementBlock{
		ID:         "announcement-b",
		FrontendID: "frontend-b",
		ChatID:     "chat-1",
		ChatType:   "group",
		BlockID:    "block-b",
	}); err != nil {
		t.Fatalf("UpsertGroupAnnouncementBlock(second frontend) error = %v", err)
	}
	if err := store.UpsertGroupAnnouncementBlock(&GroupAnnouncementBlock{
		ID:         "announcement-duplicate",
		FrontendID: "frontend-a",
		ChatID:     "chat-1",
		ChatType:   "group",
		BlockID:    "block-duplicate",
	}); err == nil {
		t.Fatal("UpsertGroupAnnouncementBlock accepted duplicate frontend/chat record")
	}
	if got := store.GroupAnnouncementBlocksByChat("frontend-a", "group", "chat-1"); len(got) != 1 || got[0].ID != "announcement-a" {
		t.Fatalf("frontend-a chat announcement blocks = %+v", got)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	if got := reopened.GetScopedGroupAnnouncementBlock("frontend-a", "announcement-a"); got == nil || got.BlockID != "block-a" || got.LastContentHash != "hash-a" {
		t.Fatalf("reopened announcement block = %+v", got)
	}
}
