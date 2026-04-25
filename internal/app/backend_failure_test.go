package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/state"
)

func TestFailSubmissionWithoutTerminalCompletionMentionsUserWhenQueueEmpty(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := appState(a).savePending(&state.PendingRequest{
		ID:         "req-1",
		Kind:       "command",
		SessionKey: "sess-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		Status:     "pending",
	}); err != nil {
		t.Fatalf("savePending() error = %v", err)
	}

	failSubmissionWithoutTerminalCompletion(a, "sess-1", sub, "thread-1", "turn-1", "Codex 后端异常退出：stdio EOF")

	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("submission after forced failure = %+v, want deleted", got)
	}
	if pending := appState(a).pending("req-1"); pending != nil {
		t.Fatalf("pending request after forced failure = %+v, want cleared", pending)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.Status != "idle" || sessionHasActiveWork(sess) {
		t.Fatalf("session after forced failure = %+v, want idle without active work", sess)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected terminal failure card")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	if !strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("terminal failure body should mention user: %q", body)
	}
	if !strings.Contains(body, "Codex 后端异常退出：stdio EOF") {
		t.Fatalf("terminal failure body = %q, want transport error text", body)
	}
}

func TestFailSubmissionWithoutTerminalCompletionSkipsMentionWhenQueuePending(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if _, err := a.store.UpdateSession("sess-1", func(sess *state.Session) {
		sess.Queue = []string{"sub-queued"}
		sess.Status = "queued"
	}); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	failSubmissionWithoutTerminalCompletion(a, "sess-1", sub, "thread-1", "turn-1", "Claude 会话异常结束：broken pipe")

	if len(ff.replyCards) == 0 {
		t.Fatal("expected terminal failure card")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	if strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("terminal failure body should not mention user while queue pending: %q", body)
	}
}

func TestFailSubmissionWithoutTerminalCompletionSuppressesTerminalStatusDuringAutoRetry(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }
	newAutoRetryService(a).AutoRetryTracker().After = func(time.Duration, func()) delayedTask {
		return &fakeDelayedTask{}
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	markSessionThreadLive(a, "sess-1", "thread-1")

	failSubmissionWithoutTerminalCompletion(a, "sess-1", sub, "thread-1", "turn-1", "Codex 后端异常退出：stdio EOF")

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

func TestClaudeHandleSessionErrorFailsRunningSubmissionOnFatalProcessExit(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "claude-thread-1", "claude-turn-1")
	runtime := newTestClaudeRuntime(t, a)
	session := &claudeSessionState{
		SessionKey: "sess-1",
		Turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "claude-turn-1"},
		},
	}
	session.SessionID = "claude-thread-1"

	runtime.service.HandleSessionError(session, claudecli.ErrorEvent{
		TurnNumber: 1,
		Error:      &claudecli.ProcessError{Message: "Claude CLI process exited", Cause: errors.New("exit status 1")},
		Context:    "stdout_eof",
	})

	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("submission after Claude fatal error = %+v, want deleted", got)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected Claude terminal failure card")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	if !strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("Claude terminal failure should mention user: %q", body)
	}
	if !strings.Contains(body, "Claude 会话异常结束") {
		t.Fatalf("Claude terminal failure body = %q, want fatal session text", body)
	}
}
