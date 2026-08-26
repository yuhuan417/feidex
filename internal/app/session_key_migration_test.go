package app

import (
	"testing"

	"feidex/internal/state"
)

func TestCanonicalizeStoredSessionKeysMigratesLegacyTypedKeys(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "bot-a"

	legacyKey := "feishu:frontend:bot-a:group:chat-1:root:root-1"
	canonicalKey := "feishu:frontend:bot-a:chat:chat-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     legacyKey,
		WorkspaceID:             "default",
		ChatID:                  "chat-1",
		ChatType:                "group",
		RootMessageID:           "root-1",
		OwnerUserID:             "user-1",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveThreadName:        "thread name",
		ActiveThreadPreview:     "thread preview",
	}); err != nil {
		t.Fatalf("UpsertSession(legacy) error = %v", err)
	}
	if err := a.store.UpsertAgentBinding(&state.AgentBinding{
		ID:          "binding-1",
		FrontendID:  "bot-a",
		ChatID:      "chat-1",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusPending.String(),
		PendingMessage: &state.AgentBindingPendingMessage{
			SessionKey:    legacyKey,
			MessageID:     "msg-1",
			ChatID:        "chat-1",
			ChatType:      "group",
			UserID:        "user-1",
			RootMessageID: "root-1",
		},
	}); err != nil {
		t.Fatalf("UpsertAgentBinding() error = %v", err)
	}

	if err := canonicalizeStoredSessionKeys(a); err != nil {
		t.Fatalf("canonicalizeStoredSessionKeys() error = %v", err)
	}

	if got := a.store.GetSession(legacyKey); got != nil {
		t.Fatalf("legacy session key still present: %+v", got)
	}
	sess := a.store.GetSession(canonicalKey)
	if sess == nil {
		t.Fatalf("expected migrated session at %q", canonicalKey)
	}
	if sess.ChatID != "chat-1" || sess.ChatType != "group" || sess.RootMessageID != "root-1" || sess.ActiveThreadID != "thread-1" {
		t.Fatalf("migrated session = %+v, want metadata and thread context preserved", sess)
	}
	binding := a.store.GetAgentBinding("binding-1")
	if binding == nil || binding.PendingMessage == nil || binding.PendingMessage.SessionKey != canonicalKey {
		t.Fatalf("binding pending session key = %+v, want %q", binding, canonicalKey)
	}
}
