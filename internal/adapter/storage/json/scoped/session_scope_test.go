package appstate

import (
	"feidex/internal/domain/conversation"
	"testing"
)

func TestSessionProjectionRejectsForeignFrontend(t *testing.T) {
	store := newTestStateStore(t)
	a, b := NewScoped(store, "a", "codex"), NewScoped(store, "b", "codex")
	key := "feishu:frontend:a:chat:chat"
	if err := a.SaveSession(&conversation.Session{Key: key, WorkspaceID: "ws"}); err != nil {
		t.Fatal(err)
	}
	if b.Session(key) != nil || len(b.Sessions()) != 0 {
		t.Fatal("foreign session exposed")
	}
	if err := b.SaveSession(&conversation.Session{Key: key}); err == nil {
		t.Fatal("foreign session save accepted")
	}
	if _, err := b.UpdateSession(key, func(current *conversation.Session) { current.WorkspaceID = "foreign" }); err == nil {
		t.Fatal("foreign mutation accepted")
	}
	if a.Session(key).WorkspaceID != "ws" {
		t.Fatal("foreign frontend changed session")
	}
}
