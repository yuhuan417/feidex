package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestHandleCommandHistoryClaudeUsesLocalTranscript(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.claude = &fakeClaudeCore{}

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	sessionID := "claude-history-1"
	writeClaudeHistoryFixture(t, configDir, a.cfg.Workspaces[0].Cwd, sessionID, time.Unix(100, 0), []string{
		fmt.Sprintf(`{"type":"custom-title","customTitle":"Claude History Session","sessionId":"%s"}`, sessionID),
		fmt.Sprintf(`{"parentUuid":null,"isSidechain":false,"promptId":"prompt-1","type":"user","message":{"role":"user","content":"hello"},"uuid":"user-1","timestamp":"2026-04-22T00:00:00Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"user-1","isSidechain":false,"message":{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"text","text":"first answer"}],"stop_reason":"end_turn"},"type":"assistant","uuid":"assistant-1","timestamp":"2026-04-22T00:00:01Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"assistant-1","isSidechain":false,"promptId":"prompt-2","type":"user","message":{"role":"user","content":"do thing"},"uuid":"user-2","timestamp":"2026-04-22T00:01:00Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"user-2","isSidechain":false,"message":{"id":"msg-2","type":"message","role":"assistant","content":[{"type":"text","text":"checking"}],"stop_reason":"tool_use"},"type":"assistant","uuid":"assistant-2a","timestamp":"2026-04-22T00:01:01Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"assistant-2a","isSidechain":false,"message":{"id":"msg-2","type":"message","role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"Bash","input":{"command":"pwd"}}],"stop_reason":"tool_use"},"type":"assistant","uuid":"assistant-2b","timestamp":"2026-04-22T00:01:02Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"assistant-2b","isSidechain":false,"promptId":"prompt-2","type":"user","message":{"role":"user","content":[{"tool_use_id":"call-1","type":"tool_result","content":"/tmp/project","is_error":false}]},"uuid":"tool-result-1","timestamp":"2026-04-22T00:01:03Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"tool-result-1","isSidechain":false,"message":{"id":"msg-2","type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"},"type":"assistant","uuid":"assistant-2c","timestamp":"2026-04-22T00:01:04Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"type":"last-prompt","lastPrompt":"do thing","sessionId":"%s"}`, sessionID),
	})

	msg := &feishu.InboundMessage{MessageID: "m-history", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          sessionID,
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadName:        "Claude History Session",
		ActiveThreadPreview:     "do thing",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := newCommandService(a).handleCommand(msg, "/history"); err != nil {
		t.Fatalf("handleCommand(/history) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after /history = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	for _, want := range []string{"当前 session: Claude History Session", "session: `claude-history-1`", "turn 数: `2`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("history body missing %q: %q", want, body)
		}
	}
	selects := cardSelectStaticForTest(ff.replyCards[0])
	if len(selects) != 1 {
		t.Fatalf("history selects = %+v, want 1", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 2 {
		t.Fatalf("history options = %+v, want 2", options)
	}
	text, _ := options[0]["text"].(map[string]any)
	label, _ := text["content"].(string)
	if !strings.Contains(label, "Turn #2 | completed | do thing") {
		t.Fatalf("history option label = %q, want latest Claude turn", label)
	}

	if err := newCommandService(a).handleCommand(msg, "/history detail 1"); err != nil {
		t.Fatalf("handleCommand(/history detail 1) error = %v", err)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after /history detail = %d, want 2", len(ff.replyCards))
	}
	detailBody := cardMarkdownContent(t, ff.replyCards[1])
	if !strings.Contains(detailBody, "Turn #1") || !strings.Contains(detailBody, "hello") || !strings.Contains(detailBody, "first answer") {
		t.Fatalf("history detail body = %q", detailBody)
	}
	if strings.Contains(detailBody, "Turn #2") {
		t.Fatalf("history detail should resolve by turn ordinal, got %q", detailBody)
	}
}

func TestRenderHistoryCardClaudeMarksLatestTurnRunningWhenSessionBusy(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude
	a.codex = nil
	a.claude = &fakeClaudeCore{}

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	sessionID := "claude-history-running"
	writeClaudeHistoryFixture(t, configDir, a.cfg.Workspaces[0].Cwd, sessionID, time.Unix(100, 0), []string{
		fmt.Sprintf(`{"parentUuid":null,"isSidechain":false,"promptId":"prompt-1","type":"user","message":{"role":"user","content":"hello"},"uuid":"user-1","timestamp":"2026-04-22T00:00:00Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"user-1","isSidechain":false,"message":{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"text","text":"first answer"}],"stop_reason":"end_turn"},"type":"assistant","uuid":"assistant-1","timestamp":"2026-04-22T00:00:01Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"assistant-1","isSidechain":false,"promptId":"prompt-2","type":"user","message":{"role":"user","content":"do thing"},"uuid":"user-2","timestamp":"2026-04-22T00:01:00Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
		fmt.Sprintf(`{"parentUuid":"user-2","isSidechain":false,"message":{"id":"msg-2","type":"message","role":"assistant","content":[{"type":"text","text":"checking"}],"stop_reason":"tool_use"},"type":"assistant","uuid":"assistant-2","timestamp":"2026-04-22T00:01:01Z","cwd":"%s","sessionId":"%s"}`, a.cfg.Workspaces[0].Cwd, sessionID),
	})

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          sessionID,
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadName:        "Claude Running Session",
		ActiveThreadPreview:     "do thing",
		ActiveTurnID:            "claude-turn-1",
		ActiveSubmissionID:      "sub-1",
		Status:                  "running",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	card, err := newHistoryService(a).renderHistoryCard(sessionKey, 0)
	if err != nil {
		t.Fatalf("renderHistoryCard() error = %v", err)
	}
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "当前 turn: `Turn #2`") {
		t.Fatalf("history body = %q, want current Claude turn marker", body)
	}
	selects := cardSelectStaticForTest(card)
	if len(selects) != 1 {
		t.Fatalf("history selects = %+v, want 1", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) < 1 {
		t.Fatalf("history options = %+v, want at least 1", options)
	}
	text, _ := options[0]["text"].(map[string]any)
	label, _ := text["content"].(string)
	if !strings.Contains(label, "当前 · Turn #2 | running | do thing") {
		t.Fatalf("history option label = %q, want running Claude turn", label)
	}
}

func writeClaudeHistoryFixture(t *testing.T, configDir, cwd, sessionID string, modTime time.Time, lines []string) string {
	t.Helper()
	projectDir := filepath.Join(configDir, "projects", sanitizeClaudeProjectDirName(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", projectDir, err)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", filePath, err)
	}
	if err := os.Chtimes(filePath, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(%s) error = %v", filePath, err)
	}
	return filePath
}
