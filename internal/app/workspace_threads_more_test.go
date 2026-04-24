package app

import (
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

func TestCreateWorkspaceAndSwitchUsesClaudeRuntimeWhenFrontendBackendIsClaude(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionID: "claude-thread-new"}
	a.claude = claude

	sessionKey := "sess-claude-create"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "p2p",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	targetDir := filepath.Join(t.TempDir(), "claude-created")
	if err := newWorkspaceManagementService(a).createWorkspaceAndSwitch(sessionKey, "user-1", "chat-1", "p2p", "claude-created", "Claude Created", targetDir); err != nil {
		t.Fatalf("createWorkspaceAndSwitch() error = %v", err)
	}

	ws := config.FindWorkspace(a.cfg, "claude-created")
	if ws == nil {
		t.Fatal("created workspace missing from config")
	}
	if len(claude.ensureCalls) != 1 || claude.ensureCalls[0].workspaceID != "claude-created" {
		t.Fatalf("Claude EnsureSession calls = %#v", claude.ensureCalls)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after workspace creation")
	}
	if sess.WorkspaceID != "claude-created" || sess.ActiveThreadID != "claude-thread-new" || sess.ActiveThreadWorkspaceID != "claude-created" {
		t.Fatalf("session after workspace creation = %+v", sess)
	}
}

func TestStartWorkspaceThreadReturnsErrorWhenCodexClientMissing(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.codex = nil

	sess := &state.Session{
		Key:         "sess-1",
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "group",
		Status:      "idle",
	}
	ws := config.FindWorkspace(a.cfg, a.cfg.Workspaces[0].ID)
	if ws == nil {
		t.Fatal("default workspace missing")
	}

	_, err := a.startWorkspaceThread("sess-1", sess, ws)
	if err == nil || !strings.Contains(err.Error(), "codex client not initialized") {
		t.Fatalf("startWorkspaceThread() error = %v, want codex client not initialized", err)
	}
}
