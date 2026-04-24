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

	resp, err := newThreadService(a).completeClaudeSessionPermissionModeSet(&feishu.CardAction{}, sessionKey, "claude-session-1", "acceptEdits")
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

func TestClaudePermissionMenusShowBypassWhenDangerousSkipPermissionsEnabled(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.backend = backendClaude
	a.cfg.Claude.DangerouslySkipPermissions = true

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

	sessionCard, err := renderClaudeSessionPermissionMenuCard(a, sessionKey)
	if err != nil {
		t.Fatalf("renderClaudeSessionPermissionMenuCard() error = %v", err)
	}
	if !cardHasButtonText(sessionCard, "bypassPermissions") {
		t.Fatalf("session permission card should expose bypassPermissions: %#v", cardButtonsForTest(sessionCard))
	}

	workspaceCard, err := renderClaudeWorkspacePermissionMenuCard(a, sessionKey)
	if err != nil {
		t.Fatalf("renderClaudeWorkspacePermissionMenuCard() error = %v", err)
	}
	if !cardHasButtonText(workspaceCard, "bypassPermissions") {
		t.Fatalf("workspace permission card should expose bypassPermissions: %#v", cardButtonsForTest(workspaceCard))
	}
	if cardHasButtonText(sessionCard, "auto") || cardHasButtonText(workspaceCard, "auto") {
		t.Fatalf("permission cards should not expose auto mode: session=%#v workspace=%#v", cardButtonsForTest(sessionCard), cardButtonsForTest(workspaceCard))
	}
}

func TestCompleteClaudeSessionPermissionModeSetRejectsBypassWhenDangerousSkipPermissionsDisabled(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.backend = backendClaude
	a.cfg.Claude.DangerouslySkipPermissions = false
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

	card, err := renderClaudeSessionPermissionMenuCard(a, sessionKey)
	if err != nil {
		t.Fatalf("renderClaudeSessionPermissionMenuCard() error = %v", err)
	}
	if cardHasButtonText(card, "bypassPermissions") {
		t.Fatalf("session permission card should hide bypassPermissions when disabled: %#v", cardButtonsForTest(card))
	}

	resp, err := newThreadService(a).completeClaudeSessionPermissionModeSet(&feishu.CardAction{}, sessionKey, "claude-session-1", "bypassPermissions")
	if err != nil {
		t.Fatalf("completeClaudeSessionPermissionModeSet() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" || !strings.Contains(resp.Toast.Content, "dangerously_skip_permissions") {
		t.Fatalf("completeClaudeSessionPermissionModeSet() response = %#v, want warning about dangerously_skip_permissions", resp)
	}
}

func TestCompleteClaudeSessionPermissionModeSetRejectsUnsupportedAutoMode(t *testing.T) {
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

	resp, err := newThreadService(a).completeClaudeSessionPermissionModeSet(&feishu.CardAction{}, sessionKey, "claude-session-1", "auto")
	if err != nil {
		t.Fatalf("completeClaudeSessionPermissionModeSet() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" || !strings.Contains(resp.Toast.Content, "不支持的 Claude 权限模式 `auto`") {
		t.Fatalf("completeClaudeSessionPermissionModeSet() response = %#v, want warning about unsupported auto mode", resp)
	}
}
