package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAgentBindingsPersistScopeAndClone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	binding := &AgentBinding{
		ID:                      " binding-a ",
		FrontendID:              " frontend-a ",
		ChatID:                  " chat-1 ",
		ChatType:                " GROUP ",
		WorkspaceID:             " workspace-a ",
		ModelOverride:           " gpt-5 ",
		ReasoningEffortOverride: " high ",
		ServiceTierOverride:     " fast ",
		SandboxModeOverride:     " read-only ",
		ApprovalPolicyOverride:  " never ",
		MultiAgentModeOverride:  " proactive ",
		ClaudePermissionMode:    " acceptEdits ",
		PendingMessage: &AgentBindingPendingMessage{
			SessionKey:       " feishu:frontend:frontend-a:group:chat-1 ",
			MessageID:        " msg-1 ",
			ChatID:           " chat-1 ",
			ChatType:         " GROUP ",
			UserID:           " user-1 ",
			Text:             "original prompt",
			RootMessageID:    " msg-1 ",
			MentionedOpenIDs: []string{" bot-a ", ""},
			Attachments: []AgentBindingPendingAttachment{{
				Kind:            " image ",
				ResourceKey:     " img-key ",
				SourceMessageID: " msg-1 ",
			}},
		},
	}
	if err := store.UpsertAgentBinding(binding); err != nil {
		t.Fatalf("UpsertAgentBinding() error = %v", err)
	}

	binding.WorkspaceID = "mutated"
	saved := store.GetScopedAgentBinding("frontend-a", "binding-a")
	if saved == nil {
		t.Fatal("GetScopedAgentBinding() returned nil")
	}
	if saved.ChatType != "group" || saved.Status != AgentBindingStatusPending.String() {
		t.Fatalf("normalized binding = %+v", saved)
	}
	if saved.WorkspaceID != "workspace-a" || saved.ModelOverride != "gpt-5" || saved.ReasoningEffortOverride != "high" {
		t.Fatalf("saved binding fields = %+v", saved)
	}
	if saved.ServiceTierOverride != "fast" || saved.SandboxModeOverride != "read-only" || saved.ApprovalPolicyOverride != "never" || saved.MultiAgentModeOverride != "proactive" || saved.ClaudePermissionMode != "acceptEdits" {
		t.Fatalf("saved binding override fields = %+v", saved)
	}
	if saved.CreatedAt == 0 || saved.UpdatedAt == 0 {
		t.Fatalf("binding timestamps = %+v", saved)
	}
	if saved.PendingMessage == nil || saved.PendingMessage.ChatType != "group" || saved.PendingMessage.Text != "original prompt" || len(saved.PendingMessage.MentionedOpenIDs) != 1 || len(saved.PendingMessage.Attachments) != 1 {
		t.Fatalf("saved pending message = %+v", saved.PendingMessage)
	}

	saved.WorkspaceID = "changed-through-return-value"
	saved.PendingMessage.Text = "changed-through-return-value"
	if got := store.GetAgentBinding("binding-a"); got == nil || got.WorkspaceID != "workspace-a" {
		t.Fatalf("GetAgentBinding() returned shared state: %+v", got)
	} else if got.PendingMessage == nil || got.PendingMessage.Text != "original prompt" {
		t.Fatalf("GetAgentBinding() returned shared pending message: %+v", got.PendingMessage)
	}

	if err := store.UpsertAgentBinding(&AgentBinding{
		ID:         "binding-b",
		FrontendID: "frontend-b",
		ChatID:     "chat-1",
		ChatType:   "group",
		Status:     AgentBindingStatusActive.String(),
	}); err != nil {
		t.Fatalf("UpsertAgentBinding(second) error = %v", err)
	}
	if err := store.UpsertAgentBinding(&AgentBinding{
		ID:         "binding-duplicate",
		FrontendID: "frontend-a",
		ChatID:     "chat-1",
		ChatType:   "group",
	}); err == nil {
		t.Fatal("UpsertAgentBinding() accepted duplicate frontend/chat binding")
	}
	if got := store.AgentBindingsByChat("frontend-a", "group", "chat-1"); len(got) != 1 || got[0].ID != "binding-a" {
		t.Fatalf("frontend-a chat bindings = %+v", got)
	}
	if got := store.AgentBindingsByChat("frontend-b", "group", "chat-1"); len(got) != 1 || got[0].ID != "binding-b" {
		t.Fatalf("frontend-b chat bindings = %+v", got)
	}
	if got := store.AgentBindingsByFrontend("frontend-a"); len(got) != 1 || got[0].ID != "binding-a" {
		t.Fatalf("frontend-a bindings = %+v", got)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	if got := reopened.GetScopedAgentBinding("frontend-a", "binding-a"); got == nil || got.WorkspaceID != "workspace-a" || got.PendingMessage == nil || got.PendingMessage.Text != "original prompt" {
		t.Fatalf("reopened binding = %+v", got)
	}
	if got := reopened.GetScopedAgentBinding("frontend-b", "binding-a"); got != nil {
		t.Fatalf("cross-frontend binding lookup = %+v, want nil", got)
	}

	if err := reopened.DeleteScopedAgentBinding("frontend-b", "binding-a"); err != nil {
		t.Fatalf("DeleteScopedAgentBinding(wrong frontend) error = %v", err)
	}
	if reopened.GetAgentBinding("binding-a") == nil {
		t.Fatal("wrong-frontend delete removed binding")
	}
	if err := reopened.DeleteScopedAgentBinding("frontend-a", "binding-a"); err != nil {
		t.Fatalf("DeleteScopedAgentBinding() error = %v", err)
	}
	if reopened.GetAgentBinding("binding-a") != nil {
		t.Fatal("DeleteScopedAgentBinding() did not remove binding")
	}
}

func TestAgentBindingsNormalizeAndMigrateSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	legacy := `{
  "version": 6,
	"sessions": {}
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open(legacy) error = %v", err)
	}
	if bindings := store.AllAgentBindings(); len(bindings) != 0 {
		t.Fatalf("legacy v6 snapshot should not synthesize agent bindings: %+v", bindings)
	}
	if primaries := store.AllGroupPrimaries(); len(primaries) != 0 {
		t.Fatalf("legacy v6 snapshot should not synthesize group primaries: %+v", primaries)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `"version": `+strconv.Itoa(currentSnapshotVersion)) {
		t.Fatalf("snapshot was not migrated to v%d:\n%s", currentSnapshotVersion, text)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		t.Fatalf("migrated snapshot is invalid JSON: %v", err)
	}
	if len(snapshot.AgentBindings) != 0 || len(snapshot.GroupPrimaries) != 0 {
		t.Fatalf("migrated snapshot synthesized new group config state: bindings=%+v primaries=%+v", snapshot.AgentBindings, snapshot.GroupPrimaries)
	}
}
