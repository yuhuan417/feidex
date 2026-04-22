package app

import (
	"strings"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCompleteClaudeSessionPermissionModeSetPersistsWithoutLiveRuntime(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.backend = backendClaude
	a.codex = nil

	runtime := newClaudeRuntime(a, a.cfg.Claude).(*claudeRuntime)
	a.claude = runtime
	defer runtime.Close()

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-session-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	resp, err := a.completeClaudeSessionPermissionModeSet(&feishu.CardAction{}, sessionKey, "claude-session-1", "acceptEdits")
	if err != nil {
		t.Fatalf("completeClaudeSessionPermissionModeSet() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeClaudeSessionPermissionModeSet() response = %#v, want success toast", resp)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("expected session to persist")
	}
	if got := strings.TrimSpace(sess.ActiveClaudePermissionMode); got != "acceptEdits" {
		t.Fatalf("ActiveClaudePermissionMode = %q, want acceptEdits", got)
	}

	if _, err := runtime.sessionState(sessionKey); err == nil {
		t.Fatal("expected runtime session to remain uninitialized in this test")
	}
}
