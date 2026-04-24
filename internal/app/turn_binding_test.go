package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func TestFindSubmissionByTurnPrefersExplicitTurnBinding(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.store.UpsertSession(&state.Session{
		Key:                "sess-1",
		WorkspaceID:        "default",
		ActiveThreadID:     "thread-1",
		ActiveSubmissionID: "sub-new",
		Status:             "turn_starting",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{ID: "sub-old", SessionKey: "sess-1", WorkspaceID: "default", ThreadID: "thread-1", TurnID: "turn-old", Status: "completed"}); err != nil {
		t.Fatalf("CreateSubmission(sub-old) error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{ID: "sub-new", SessionKey: "sess-1", WorkspaceID: "default", ThreadID: "thread-1", Status: "running"}); err != nil {
		t.Fatalf("CreateSubmission(sub-new) error = %v", err)
	}
	a.bindTurnSubmission("thread-1", "turn-old", "sess-1", "sub-old")

	sessionKey, sub := a.findSubmissionByTurn("thread-1", "turn-old")
	if sessionKey != "sess-1" || sub == nil || sub.ID != "sub-old" {
		t.Fatalf("findSubmissionByTurn() = %q %+v, want sub-old", sessionKey, sub)
	}
}

func TestFinishTurnCompletedWithoutFinalSendsEmptyGreenCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.Status = "running"
	}); err != nil {
		t.Fatalf("UpdateSubmission() error = %v", err)
	}

	a.noteTurnStarted("sess-1", &state.Submission{ID: sub.ID, SessionKey: "sess-1", WorkspaceID: "default", ThreadID: "thread-1", TurnID: "turn-1"})
	a.finishTurn("thread-1", "turn-1", "completed")

	if len(ff.replyCards) == 0 {
		t.Fatal("expected empty green final card to be sent")
	}
	if len(ff.replyTextWithIDs) != 0 {
		t.Fatalf("did not expect old output text to be resent, got %+v", ff.replyTextWithIDs)
	}
	card := ff.replyCards[len(ff.replyCards)-1]
	header, _ := card["header"].(map[string]any)
	if got, _ := header["template"].(string); got != "green" {
		t.Fatalf("final fallback card template = %q, want green", got)
	}
}

func TestFinalAnswersAreSentImmediatelyAndNotReplayedOnCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.markTurnStartedAt("turn-1", time.Now())
	a.noteTurnStarted("sess-1", sub)

	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "item-1", map[string]any{
		"type":  "agent_message",
		"text":  "first final",
		"phase": "final_answer",
	})
	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "item-2", map[string]any{
		"type":  "agent_message",
		"text":  "second final",
		"phase": "final_answer",
	})

	if len(ff.replyCards) != 2 {
		t.Fatalf("expected both final cards to be sent immediately, got %d", len(ff.replyCards))
	}
	a.finishTurn("thread-1", "turn-1", "completed")
	if len(ff.replyCards) != 2 {
		t.Fatalf("expected no replay on completion, got %d cards", len(ff.replyCards))
	}
}

func TestFinishTurnFailedAutoRetrySuppressesTerminalStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }
	a.autoRetryTracker().after = func(time.Duration, func()) delayedTask {
		return &fakeDelayedTask{}
	}
	if err := a.updateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.markSessionThreadLive("sess-1", "thread-1")

	a.finishTurn("thread-1", "turn-1", "failed")

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply cards = %d, want 1 auto-retry card only", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != "Codex 自动重试" {
		t.Fatalf("reply card title = %q, want Codex 自动重试", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "自动发送“继续”") {
		t.Fatalf("auto retry card body = %q", body)
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched cards = %d, want 0", len(ff.patchedCards))
	}
}

func TestBindTurnSubmissionRebindClearsMetadataState(t *testing.T) {
	a, _, _ := newTestApp(t)
	startedAt := time.Now().Add(-2 * time.Second)

	a.bindTurnSubmission("thread-old", "turn-1", "sess-old", "sub-old")
	a.markTurnStartedAt("turn-1", startedAt)
	a.recordTurnTokenUsage("thread-old", "turn-1", codexrpc.ThreadTokenUsage{
		Last: codexrpc.TokenUsageBreakdown{
			InputTokens: 42,
		},
	})

	a.bindTurnSubmission("thread-new", "turn-1", "sess-new", "sub-new")

	if usageLine, contextLine, elapsedLine := a.turnFinalMetadata("turn-1", time.Now()); usageLine != "" || contextLine != "" || elapsedLine != "" {
		t.Fatalf("turnFinalMetadata() after rebind = %q, %q, %q, want all empty", usageLine, contextLine, elapsedLine)
	}
}
