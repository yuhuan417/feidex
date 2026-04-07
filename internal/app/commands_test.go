package app

import (
	"strings"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandNewRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	err = a.commandNew(&feishu.InboundMessage{
		ChatID:   "chat",
		ChatType: "p2p",
		UserID:   "user",
	})
	if err == nil {
		t.Fatal("expected running turn to block /new")
	}
	if !strings.Contains(err.Error(), "仍在运行") {
		t.Fatalf("unexpected error: %v", err)
	}
}
