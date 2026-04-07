package app

import (
	"testing"

	"feidex/internal/state"
)

func TestCompleteMenuInterruptRejectsStaleTurnCard(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:          "sess-1",
		ActiveThreadID: "thread-new",
		ActiveTurnID: "turn-new",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeMenuInterrupt(nil, "sess-1", "turn-old")
	if err != nil {
		t.Fatalf("completeMenuInterrupt: %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatal("expected warning toast")
	}
	if resp.Toast.Type != "warning" {
		t.Fatalf("unexpected toast type: %q", resp.Toast.Type)
	}
}
