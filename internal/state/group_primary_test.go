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
	if err := store.UpsertGroupPrimary(&GroupPrimary{
		ID:             " primary-a ",
		ChatID:         " chat-1 ",
		ChatType:       " GROUP ",
		OwnerBotOpenID: " bot-a ",
	}); err != nil {
		t.Fatalf("UpsertGroupPrimary() error = %v", err)
	}
	gotA := store.GetGroupPrimary("primary-a")
	if gotA == nil || gotA.ChatID != "chat-1" || gotA.ChatType != "group" || gotA.OwnerBotOpenID != "bot-a" {
		t.Fatalf("frontend-a group primary = %+v", gotA)
	}
	gotA.OwnerBotOpenID = "mutated"
	if again := store.GetGroupPrimary("primary-a"); again == nil || again.OwnerBotOpenID != "bot-a" {
		t.Fatalf("GetScopedGroupPrimary returned shared state: %+v", again)
	}

	if err := store.UpsertGroupPrimary(&GroupPrimary{
		ID:             "primary-b",
		ChatID:         "chat-1",
		ChatType:       "group",
		OwnerBotOpenID: "bot-b",
	}); err == nil {
		t.Fatal("UpsertGroupPrimary accepted a second owner record for the same chat")
	}
	if got := store.GroupPrimariesByChat("ignored", "group", "chat-1"); len(got) != 1 || got[0].OwnerBotOpenID != "bot-a" {
		t.Fatalf("GroupPrimariesByChat() = %+v, want one owner", got)
	}
}
