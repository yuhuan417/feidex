package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestListClaudeSessionsFiltersWorkspaceAndSortsRecent(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspace := &a.cfg.Workspaces[0]
	altCwd := t.TempDir()
	writeClaudeSessionFixture(t, configDir, workspace.Cwd, "session-older", "", "older prompt", time.Unix(100, 0))
	writeClaudeSessionFixture(t, configDir, workspace.Cwd, "session-newer", "Named Session", "newer prompt", time.Unix(200, 0))
	writeClaudeSessionFixture(t, configDir, altCwd, "session-alt", "Alt Session", "alt prompt", time.Unix(300, 0))

	items, err := a.listClaudeSessions("sess-1", workspace, false)
	if err != nil {
		t.Fatalf("listClaudeSessions(workspace) error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("listClaudeSessions(workspace) count = %d, want 2", len(items))
	}
	if items[0].ID != "session-newer" || items[0].Name != "Named Session" || items[0].Preview != "newer prompt" {
		t.Fatalf("listClaudeSessions(workspace)[0] = %+v", items[0])
	}
	for _, item := range items {
		if item.ID == "session-alt" {
			t.Fatalf("listClaudeSessions(workspace) should filter alt session: %+v", items)
		}
	}

	itemsAll, err := a.listClaudeSessions("sess-1", workspace, true)
	if err != nil {
		t.Fatalf("listClaudeSessions(all) error = %v", err)
	}
	if len(itemsAll) != 3 {
		t.Fatalf("listClaudeSessions(all) count = %d, want 3", len(itemsAll))
	}
	if itemsAll[0].ID != "session-alt" {
		t.Fatalf("listClaudeSessions(all)[0] = %+v, want alt session first by mtime", itemsAll[0])
	}
}

func TestHandleCommandSessionListClaudeShowsSessionCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.claude = &fakeClaudeCore{}

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	writeClaudeSessionFixture(t, configDir, a.cfg.Workspaces[0].Cwd, "session-list-1", "List Session", "continue work", time.Unix(100, 0))

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	if err := newCommandService(a).handleCommand(msg, "/session"); err != nil {
		t.Fatalf("handleCommand(/session) error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected Claude session list card to be sent")
	}
	card := ff.replyCards[len(ff.replyCards)-1]
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "当前 backend: `claude`") || !strings.Contains(body, "通过下拉 list 选择要切换的 Claude 会话。") || !strings.Contains(body, "开始对话后") {
		t.Fatalf("Claude session list body = %q", body)
	}
	if selects := cardSelectStaticForTest(card); len(selects) != 1 {
		t.Fatalf("Claude session list selects = %+v, want 1", selects)
	}
}

func TestRenderClaudeThreadsCardShowsForkAndShortIDsForActiveSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.claude = &fakeClaudeCore{}

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	sessionID := "12345678abcdef-session"
	writeClaudeSessionFixture(t, configDir, a.cfg.Workspaces[0].Cwd, sessionID, "Claude Session", "continue work", time.Unix(100, 0))

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          sessionID,
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadName:        "Claude Session",
		ActiveThreadPreview:     "continue work",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	card, err := a.renderThreadsCard(sessionKey, false)
	if err != nil {
		t.Fatalf("renderThreadsCard() error = %v", err)
	}
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "开始对话后") {
		t.Fatalf("Claude thread card body = %q, want warmup hint", body)
	}
	labels := cardButtonLabelsByAction(card)
	if _, ok := labels["menu.fork"]; !ok {
		t.Fatalf("Claude thread card missing fork button: %+v", labels)
	}
	if _, ok := labels["thread.sandbox.menu"]; ok {
		t.Fatalf("Claude thread card should not expose thread sandbox menu: %+v", labels)
	}
	if _, ok := labels["thread.policy.menu"]; ok {
		t.Fatalf("Claude thread card should not expose thread policy menu: %+v", labels)
	}
	selects := cardSelectStaticForTest(card)
	if len(selects) != 1 {
		t.Fatalf("Claude thread card selects = %+v, want 1", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 1 {
		t.Fatalf("Claude thread card options = %+v, want 1", options)
	}
	text, _ := options[0]["text"].(map[string]any)
	label, _ := text["content"].(string)
	if !strings.Contains(label, "[12345678]") {
		t.Fatalf("Claude thread option label = %q, want short id", label)
	}
}

func TestHandleCommandSessionResumeClaudeResumesSession(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	claude := &fakeClaudeCore{ensureSessionSet: true, ensureSessionID: "session-resume-1"}
	a.claude = claude

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	writeClaudeSessionFixture(t, configDir, a.cfg.Workspaces[0].Cwd, "session-resume-1", "Resume Session", "please continue", time.Unix(100, 0))

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	sessionKey := a.makeSessionKey(msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "group",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := newCommandService(a).handleCommand(msg, "/session resume session-resume-1"); err != nil {
		t.Fatalf("handleCommand(/session resume) error = %v", err)
	}
	if len(claude.ensureCalls) != 1 {
		t.Fatalf("Claude EnsureSession calls = %#v, want 1", claude.ensureCalls)
	}
	if claude.ensureCalls[0].resumeID != "session-resume-1" || claude.ensureCalls[0].workspaceID != a.cfg.Workspaces[0].ID {
		t.Fatalf("Claude EnsureSession call = %#v", claude.ensureCalls[0])
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after Claude resume")
	}
	if sess.ActiveThreadID != "session-resume-1" || sess.ActiveThreadName != "Resume Session" || sess.ActiveThreadPreview != "please continue" {
		t.Fatalf("session after Claude resume = %+v", sess)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected resume command to reply with thread card")
	}
}

func TestCompleteThreadResumeClaudeRejectsSessionFromDifferentWorkspace(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.claude = &fakeClaudeCore{}
	altCwd := t.TempDir()
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Name: "Alt", Cwd: altCwd, ApprovalPolicy: "never", SandboxMode: "read-only"})

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	writeClaudeSessionFixture(t, configDir, altCwd, "session-alt-1", "Alt Session", "alt prompt", time.Unix(100, 0))

	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "group",
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	resp, err := newThreadActionService(a).completeThreadResume(&feishu.CardAction{UserID: "user-1", ChatID: "chat-1"}, sessionKey, "session-alt-1")
	if err != nil {
		t.Fatalf("completeThreadResume() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeThreadResume() response = %#v, want warning toast", resp)
	}
	if got := a.store.GetSession(sessionKey); got == nil || got.ActiveThreadID != "" {
		t.Fatalf("session after rejected Claude resume = %+v, want unchanged", got)
	}
}

func writeClaudeSessionFixture(t *testing.T, configDir, cwd, sessionID, title, lastPrompt string, modTime time.Time) string {
	t.Helper()
	projectDir := filepath.Join(configDir, "projects", sanitizeClaudeProjectDirName(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", projectDir, err)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	lines := make([]string, 0, 4)
	if strings.TrimSpace(title) != "" {
		lines = append(lines, `{"type":"custom-title","customTitle":"`+title+`","sessionId":"`+sessionID+`"}`)
	}
	lines = append(lines,
		`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"`+lastPrompt+`"},"uuid":"user-1","timestamp":"2026-04-22T00:00:00Z","cwd":"`+cwd+`","sessionId":"`+sessionID+`"}`,
		`{"type":"last-prompt","lastPrompt":"`+lastPrompt+`","sessionId":"`+sessionID+`"}`,
	)
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", filePath, err)
	}
	if err := os.Chtimes(filePath, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(%s) error = %v", filePath, err)
	}
	return filePath
}
