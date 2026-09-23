package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

// deliverTestPendingUserInputCard delivers a card that waits for user input,
// mirroring the shared chokepoint both backends use for question/approval cards.
func deliverTestPendingUserInputCard(t *testing.T, a *App, sub *state.Submission) {
	t.Helper()
	if err := deliverPendingCard(a, sub, a.feishu.SimpleStatusCard("需要补充输入", "orange", "你希望用哪种方案？", nil), pendingCardDelivery{
		requestKey:    "req-1",
		backend:       backendCodex,
		kind:          "tool_request_user_input",
		sessionKey:    sub.SessionKey,
		threadID:      sub.ThreadID,
		turnID:        sub.TurnID,
		itemID:        "item-1",
		ownerUserID:   sub.UserID,
		payloadJSON:   "{}",
		waitingStatus: state.SubmissionStatusWaitingUserInput.String(),
		linkKind:      "user_input_card",
	}); err != nil {
		t.Fatalf("deliverPendingCard() error = %v", err)
	}
}

func completeQuietCommandItem(t *testing.T, a *App, workspace, itemID, path string) {
	t.Helper()
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", itemID, map[string]any{
		"id":     itemID,
		"type":   "commandExecution",
		"status": "completed",
		"cwd":    workspace,
		"commandActions": []any{
			map[string]any{"type": "read", "path": filepath.Join(workspace, path)},
		},
	})
}

// Progress must only be patched onto the newest card while that card is still
// the progress card. Once a card that waits for user input has been delivered,
// the next progress update has to start a new card instead of rewriting the
// card that is now above it.
func TestQuietWorkingCardStartsNewCardAfterPendingUserInputCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	newTurnStreamService(a).noteTurnStarted("sess-1", sub)

	completeQuietCommandItem(t, a, workspace, "cmd-1", "first.go")
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after first progress = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "Read `first.go`") {
		t.Fatalf("working card body = %q, want it to contain the first progress line", body)
	}

	deliverTestPendingUserInputCard(t, a, sub)
	repliesAfterQuestion := len(ff.replyCards)

	completeQuietCommandItem(t, a, workspace, "cmd-2", "second.go")
	if got := len(ff.replyCards) - repliesAfterQuestion; got != 1 {
		t.Fatalf("reply cards added after answering = %d, want 1 (progress must not be patched into the card delivered before the question)", got)
	}
	newest := ff.replyCards[len(ff.replyCards)-1]
	if body := cardMarkdownContent(t, newest); !strings.Contains(body, "Read `second.go`") {
		t.Fatalf("new progress card body = %q, want it to contain the resumed progress line", body)
	}
	if body := cardMarkdownContent(t, newest); strings.Contains(body, "你希望用哪种方案？") {
		t.Fatalf("progress must not reuse the user input card body: %q", body)
	}
}

// A reasoning-only working card carries no content worth keeping, so the delayed
// user input card may still replace it in place; progress after that must again
// start from a fresh card rather than adopting the question card's message.
func TestQuietWorkingCardReusesReasoningOnlyCardThenStartsFresh(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	newTurnStreamService(a).noteTurnStarted("sess-1", sub)

	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "reason-1", map[string]any{
		"id":   "reason-1",
		"type": "reasoning",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after reasoning = %d, want 1", len(ff.replyCards))
	}

	deliverTestPendingUserInputCard(t, a, sub)
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after question = %d, want 1 (reasoning-only card is replaced in place)", len(ff.patchedCards))
	}
	repliesAfterQuestion := len(ff.replyCards)

	completeQuietCommandItem(t, a, workspace, "cmd-1", "first.go")
	if got := len(ff.replyCards) - repliesAfterQuestion; got != 1 {
		t.Fatalf("reply cards added after answering = %d, want 1", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "Read `first.go`") {
		t.Fatalf("new progress card body = %q", body)
	}
}
