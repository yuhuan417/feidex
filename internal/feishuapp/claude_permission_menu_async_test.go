package feishuapp

import (
	"errors"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"strings"
	"testing"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/feishu"
)

// newClaudePermissionMenuApp prepares an app on the Claude backend with an
// active session whose thread matches the card's thread id.
func newClaudePermissionMenuApp(t *testing.T, claude *fakeClaudeCore) (*App, *fakeFeishuClient, string) {
	t.Helper()
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	a.runtimeView().setCodex(nil)
	a.runtimeView().setClaudeCore(claude)
	sessionKey := "feishu:chat:chat-1"
	if err := a.store.UpsertSession(&conversation.Session{
		Key:            sessionKey,
		ChatID:         "chat-1",
		ChatType:       "p2p",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "claude-thread-1",
		OwnerUserID:    "user-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	return a, ff, sessionKey
}

func dispatchClaudePermissionModeSet(t *testing.T, a *App, sessionKey, mode, messageID string) {
	t.Helper()
	if _, err := newCardActionService(a).dispatch(&feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: messageID,
		ActionValue: map[string]any{
			"action":      "thread.permission_mode.set",
			"session_key": sessionKey,
			"thread_id":   "claude-thread-1",
			"mode":        mode,
		},
	}); err != nil {
		t.Fatalf("dispatch(thread.permission_mode.set) error = %v", err)
	}
}

// The card callback must answer without waiting for the CLI round-trip, and
// the runtime apply must still happen afterwards.
func TestClaudePermissionMenuAppliesRuntimeAsynchronously(t *testing.T) {
	claude := &fakeClaudeCore{}
	a, ff, sessionKey := newClaudePermissionMenuApp(t, claude)

	resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: "msg-menu-1",
		ActionValue: map[string]any{
			"action":      "thread.permission_mode.set",
			"session_key": sessionKey,
			"thread_id":   "claude-thread-1",
			"mode":        "acceptEdits",
		},
	})
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || !strings.Contains(resp.Toast.Content, "已更新") {
		t.Fatalf("response = %#v, want an immediate success toast", resp)
	}
	if resp.Card == nil {
		t.Fatal("response should render the permission menu immediately")
	}
	a.waitAsync()

	if len(ff.patchedCards) != 0 {
		t.Fatal("no failure patch expected on the happy path")
	}

	if len(claude.permissionModeCalls) != 1 {
		t.Fatalf("runtime apply calls = %#v, want one async apply", claude.permissionModeCalls)
	}
	if got := claude.permissionModeCalls[0].mode; got != "acceptEdits" {
		t.Fatalf("applied mode = %q, want acceptEdits", got)
	}
}

// A runtime apply that fails after the ack must patch the menu card instead of
// leaving it claiming a change that never landed.
func TestClaudePermissionMenuRuntimeFailurePatchesCard(t *testing.T) {
	claude := &fakeClaudeCore{permissionModeErr: errors.New("control request failed: session gone")}
	a, ff, sessionKey := newClaudePermissionMenuApp(t, claude)

	dispatchClaudePermissionModeSet(t, a, sessionKey, "default", "msg-menu-2")
	a.waitAsync()

	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want the menu card patched with a warning", len(ff.patchedCards))
	}
	body := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(body, "运行时未生效") || !strings.Contains(body, "设置已保存") {
		t.Fatalf("patched card body = %q, want the runtime failure warning", body)
	}
	if !strings.Contains(body, "权限模式") {
		t.Fatalf("patched card body = %q, want the menu body preserved", body)
	}
}

// The slash command path has no ack deadline and must keep applying
// synchronously so its reply reflects the real result.
func TestClaudePermissionCommandAppliesRuntimeSynchronously(t *testing.T) {
	claude := &fakeClaudeCore{}
	a, _, _ := newClaudePermissionMenuApp(t, claude)

	err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandSession(&feishu.InboundMessage{
		MessageID: "msg-command-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
	}, []string{"permissions", "acceptEdits"})
	if err != nil {
		t.Fatalf("CommandSession(permissions) error = %v", err)
	}
	if len(claude.permissionModeCalls) != 1 {
		t.Fatalf("runtime apply calls = %#v, want one synchronous apply", claude.permissionModeCalls)
	}
}
