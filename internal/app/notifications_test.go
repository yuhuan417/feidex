package app

import (
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestRenderSubmissionCardShowsFullContentInCard(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg, feishu: feishu.New(cfg.Feishu)}

	reply := strings.Repeat("hello ", 120)
	command := strings.Repeat("output\n", 80)
	sub := &state.Submission{
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		InputText:   "show me everything",
		CommandText: command,
		OutputText:  reply,
		Status:      "running",
	}

	card := a.renderSubmissionCard(sub, sub.Status)
	elements, ok := card["elements"].([]map[string]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card["elements"])
	}
	body, ok := elements[0]["content"].(string)
	if !ok {
		t.Fatalf("unexpected card content: %#v", elements[0]["content"])
	}
	if !strings.Contains(body, "输入:\nshow me everything") {
		t.Fatalf("input missing from body: %q", body)
	}
	if !strings.Contains(body, "命令输出:\n"+strings.TrimSpace(command)) {
		t.Fatalf("command output missing from body: %q", body)
	}
	if !strings.Contains(body, "回复:\n"+strings.TrimSpace(reply)) {
		t.Fatalf("reply missing from body: %q", body)
	}
	if strings.Contains(body, "调试:") {
		t.Fatalf("debug block should not be present: %q", body)
	}
}

func TestNormalizeCardMarkdownNormalizesFenceSyntax(t *testing.T) {
	got := normalizeCardMarkdown("```txt\nhello")
	want := "```\nhello\n```"
	if got != want {
		t.Fatalf("unexpected normalized markdown:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestApprovalButtonsOmitCancel(t *testing.T) {
	buttons := approvalButtons("command", "req-1")
	if len(buttons) != 3 {
		t.Fatalf("expected 3 approval buttons, got %d", len(buttons))
	}
	for _, btn := range buttons {
		if btn.Text == "取消" {
			t.Fatalf("expected cancel button to be omitted, got %#v", buttons)
		}
	}
}

func TestTurnCompletionMessagesAlwaysNotifyInterrupted(t *testing.T) {
	replyText, terminalText := turnCompletionMessages("interrupted", "partial answer", "", false)
	if replyText != "partial answer" {
		t.Fatalf("unexpected reply text: %q", replyText)
	}
	if terminalText != "任务已中断。" {
		t.Fatalf("unexpected interrupted terminal text: %q", terminalText)
	}

	replyText, terminalText = turnCompletionMessages("interrupted", "partial answer", "", true)
	if replyText != "" {
		t.Fatalf("expected no reply resend after output already sent, got %q", replyText)
	}
	if terminalText != "任务已中断。" {
		t.Fatalf("expected interrupted notification even after output sent, got %q", terminalText)
	}
}

func TestTurnCompletionMessagesKeepsCompletedSilent(t *testing.T) {
	replyText, terminalText := turnCompletionMessages("completed", "final answer", "", false)
	if replyText != "final answer" {
		t.Fatalf("unexpected completed reply text: %q", replyText)
	}
	if terminalText != "" {
		t.Fatalf("expected no terminal notice for completed turn, got %q", terminalText)
	}
}

func TestTurnStartedNotificationRebindsPendingSubmission(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store, turnStreams: map[string]*turnStream{}}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveSubmissionID:      "sub-1",
		Status:                  "turn_starting",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	_, err = a.store.CreateSubmission(&state.Submission{
		ID:          "sub-1",
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		Status:      "queued",
	})
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}

	a.onTurnStartedNotification("thread-1", "turn-1")

	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.ActiveTurnID != "turn-1" || sess.Status != "turn_in_progress" {
		t.Fatalf("unexpected rebound session: %#v", sess)
	}
	sub := a.store.GetSubmission("sub-1")
	if sub == nil {
		t.Fatal("expected submission")
	}
	if sub.TurnID != "turn-1" || sub.ThreadID != "thread-1" || sub.Status != "running" {
		t.Fatalf("unexpected rebound submission: %#v", sub)
	}
}

func TestFindSubmissionByTurnFallsBackToActiveSubmissionOnThread(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:                "sess-1",
		WorkspaceID:        "default",
		ActiveThreadID:     "thread-1",
		ActiveSubmissionID: "sub-1",
		Status:             "turn_starting",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	_, err = a.store.CreateSubmission(&state.Submission{
		ID:          "sub-1",
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		Status:      "queued",
	})
	if err != nil {
		t.Fatalf("create submission: %v", err)
	}
	sessionKey, sub := a.findSubmissionByTurn("thread-1", "turn-1")
	if sessionKey != "sess-1" || sub == nil || sub.ID != "sub-1" {
		t.Fatalf("unexpected fallback result: session=%q sub=%#v", sessionKey, sub)
	}
}
