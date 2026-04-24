package app

import (
	appapproval "feidex/internal/app/approval"
	"strings"
	"testing"

	"feidex/internal/state"
)

func TestNormalizeCardMarkdownOnlyTrimsWhitespace(t *testing.T) {
	input := "\n\n```txt\nhello\n```\n\n"
	got := normalizeCardMarkdown(input)
	want := "```txt\nhello\n```"
	if got != want {
		t.Fatalf("normalizeCardMarkdown() should only trim, got: %q", got)
	}
}

func TestMarkdownCodeBlockWithLangUsesDynamicOuterFence(t *testing.T) {
	got := normalizeCardMarkdown("命令:\n" + markdownCodeBlockWithLang("bash", "pwd"))
	if !strings.Contains(got, "````bash\npwd\n````") {
		t.Fatalf("simple content should keep 4-backtick outer fence, got: %q", got)
	}
	got = markdownCodeBlockWithLang("", "keep ```` inner")
	if !strings.HasPrefix(got, "`````\n") || !strings.HasSuffix(got, "\n`````") {
		t.Fatalf("outer fence should be inner max+1 when inner has 4 backticks, got: %q", got)
	}
}

func TestApprovalButtonsCoverFullDecisionSet(t *testing.T) {
	commandButtons := appapproval.Buttons("command", "req-1")
	if len(commandButtons) != 4 {
		t.Fatalf("expected 4 command approval buttons, got %d", len(commandButtons))
	}
	commandTexts := map[string]bool{}
	for _, btn := range commandButtons {
		commandTexts[btn.Text] = true
	}
	for _, want := range []string{
		"允许一次",
		"本会话允许",
		"拒绝",
		"拒绝并中断",
	} {
		if !commandTexts[want] {
			t.Fatalf("missing command approval button %q in %#v", want, commandButtons)
		}
	}

	fileButtons := appapproval.Buttons("file", "req-2")
	if len(fileButtons) != 4 {
		t.Fatalf("expected 4 file approval buttons, got %d", len(fileButtons))
	}
	fileTexts := map[string]bool{}
	for _, btn := range fileButtons {
		fileTexts[btn.Text] = true
	}
	for _, want := range []string{"允许一次", "本会话允许", "拒绝", "拒绝并中断"} {
		if !fileTexts[want] {
			t.Fatalf("missing file approval button %q in %#v", want, fileButtons)
		}
	}
}

func TestTurnCompletionTerminalTextAlwaysNotifyInterrupted(t *testing.T) {
	terminalText := turnCompletionTerminalText("interrupted", "")
	if terminalText != "任务已中断。" {
		t.Fatalf("unexpected interrupted terminal text: %q", terminalText)
	}
}

func TestTurnCompletionTerminalTextKeepsCompletedSilent(t *testing.T) {
	terminalText := turnCompletionTerminalText("completed", "")
	if terminalText != "" {
		t.Fatalf("expected no terminal notice for completed turn, got %q", terminalText)
	}
}

func TestTurnStartedNotificationRebindsPendingSubmission(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store, turnStreams: newTurnStreamTracker()}
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

	onTurnStartedNotification(a, "thread-1", "turn-1")

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
	sessionKey, sub := findSubmissionByTurn(a, "thread-1", "")
	if sessionKey != "sess-1" || sub == nil || sub.ID != "sub-1" {
		t.Fatalf("unexpected fallback result: session=%q sub=%#v", sessionKey, sub)
	}
}
