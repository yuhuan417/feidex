package app

import (
	"path/filepath"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

func TestResolveSubmissionWorkspaceUsesLocalBindingOnly(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	a := &App{cfg: cfg, store: store, frontendID: "frontend-a"}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-client",
		ChatID:      "chat-1",
		ChatType:    "group",
		WorkspaceID: "client-workspace",
		Status:      state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	sess := &state.Session{
		Key:       "feishu:frontend:frontend-a:group:chat-1",
		BindingID: "binding-client",
	}
	if got := resolveSubmissionWorkspaceID(a, nil, sess, false); got != "client-workspace" {
		t.Fatalf("binding workspace = %q, want client-workspace", got)
	}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-empty",
		ChatID:   "chat-2",
		ChatType: "group",
		Status:   state.AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding(empty) error = %v", err)
	}
	emptySession := &state.Session{
		Key:         "feishu:frontend:frontend-a:group:chat-2",
		BindingID:   "binding-empty",
		WorkspaceID: "",
	}
	if got := resolveSubmissionWorkspaceID(a, nil, emptySession, false); got != "" {
		t.Fatalf("empty binding workspace = %q, want empty", got)
	}
}
