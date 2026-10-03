package appstate

import (
	"testing"

	"feidex/internal/state"
)

func TestAgentBindingFrontendScope(t *testing.T) {
	store := newTestStateStore(t)
	frontendA := NewScoped(store, "frontend-a", "")
	frontendB := NewScoped(store, "frontend-b", "")

	if err := frontendA.SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-a",
		ChatID:   "chat-1",
		ChatType: "group",
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	if got := frontendA.AgentBinding("binding-a"); got == nil || got.FrontendID != "frontend-a" {
		t.Fatalf("frontend-a binding = %+v", got)
	}
	if got := frontendB.AgentBinding("binding-a"); got != nil {
		t.Fatalf("frontend-b saw frontend-a binding = %+v", got)
	}

	if err := frontendB.SaveAgentBinding(&state.AgentBinding{
		ID:         "binding-b",
		FrontendID: "frontend-a",
		ChatID:     "chat-2",
	}); err == nil {
		t.Fatal("SaveAgentBinding() accepted a different frontend")
	}
	if err := store.UpsertAgentBinding(&state.AgentBinding{
		ID:       "binding-b",
		ChatID:   "chat-1",
		ChatType: "group",
	}); err != nil {
		t.Fatalf("UpsertAgentBinding(legacy) error = %v", err)
	}
	if got := frontendA.AgentBinding("binding-b"); got != nil {
		t.Fatalf("frontend-a saw an unscoped binding = %+v", got)
	}
	if got := frontendA.AgentBindingsForChat("group", "chat-1"); len(got) != 1 {
		t.Fatalf("frontend-a chat bindings = %+v", got)
	}

	if err := frontendA.DeleteAgentBinding("binding-b"); err != nil {
		t.Fatalf("DeleteAgentBinding(legacy) error = %v", err)
	}
	if store.GetAgentBinding("binding-b") == nil {
		t.Fatal("non-legacy frontend unexpectedly deleted blank binding")
	}
}

func newTestStateStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	return store
}
