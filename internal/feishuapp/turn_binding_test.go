package feishuapp

import (
	domainsubmission "feidex/internal/domain/submission"

	"context"
	"encoding/json"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/adapter/feishu/turnitem"
	appautoretry "feidex/internal/application/autoretry"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	"strings"
	"testing"
	"time"
)

func TestFindSubmissionByTurnPrefersExplicitTurnBinding(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.store.UpsertSession(&conversation.Session{
		Key:                "sess-1",
		WorkspaceID:        "default",
		ActiveThreadID:     "thread-1",
		ActiveSubmissionID: "sub-new",
		Status:             "turn_starting",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&domainsubmission.Submission{ID: "sub-old", SessionKey: "sess-1", WorkspaceID: "default", ThreadID: "thread-1", TurnID: "turn-old", Status: "completed"}); err != nil {
		t.Fatalf("CreateSubmission(sub-old) error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&domainsubmission.Submission{ID: "sub-new", SessionKey: "sess-1", WorkspaceID: "default", ThreadID: "thread-1", Status: "running"}); err != nil {
		t.Fatalf("CreateSubmission(sub-new) error = %v", err)
	}
	a.runtimeOwner.TurnBindings.BindTurnSubmission("thread-1", "turn-old", "sess-1", "sub-old")

	sessionKey, sub := a.bindings.Submissions.FindSubmissionByTurn("thread-1", "turn-old")
	if sessionKey != "sess-1" || sub == nil || sub.ID != "sub-old" {
		t.Fatalf("findSubmissionByTurn() = %q %+v, want sub-old", sessionKey, sub)
	}
}

func TestFinishTurnCompletedWithoutFinalSendsEmptyGreenCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := a.store.UpdateSubmission(sub.ID, func(s *domainsubmission.Submission) {
		s.Status = "running"
	}); err != nil {
		t.Fatalf("UpdateSubmission() error = %v", err)
	}

	a.bindings.TurnPresentation.NoteTurnStarted("sess-1", &domainsubmission.Submission{ID: sub.ID, SessionKey: "sess-1", WorkspaceID: "default", ThreadID: "thread-1", TurnID: "turn-1"})
	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")

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
	a.runtimeOwner.TurnBindings.BindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.runtimeOwner.TurnBindings.MarkTurnStartedAt("turn-1", time.Now())
	a.bindings.TurnPresentation.NoteTurnStarted("sess-1", sub)

	a.bindings.TurnPresentation.CompleteTurnItem(context.Background(), "thread-1", "turn-1", "item-1", turnitem.NewProtocolItemWithID("item-1", map[string]any{
		"type":  "agent_message",
		"text":  "first final",
		"phase": "final_answer",
	}))
	a.bindings.TurnPresentation.CompleteTurnItem(context.Background(), "thread-1", "turn-1", "item-2", turnitem.NewProtocolItemWithID("item-2", map[string]any{
		"type":  "agent_message",
		"text":  "second final",
		"phase": "final_answer",
	}))

	if len(ff.replyCards) != 1 {
		t.Fatalf("expected first final card to be sent immediately, got %d", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("expected second final answer to patch the first final card, got %d patches", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "second final") {
		t.Fatalf("patched final body = %q, want second final", body)
	}
	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")
	if len(ff.replyCards) != 1 || len(ff.patchedCards) != 1 {
		t.Fatalf("expected no replay on completion, got %d cards and %d patches", len(ff.replyCards), len(ff.patchedCards))
	}
}

func TestLegacyAgentMessageWithoutPhaseIsFinalAndNotReplayedOnCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.runtimeOwner.TurnBindings.BindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.runtimeOwner.TurnBindings.MarkTurnStartedAt("turn-1", time.Now())
	a.bindings.TurnPresentation.NoteTurnStarted("sess-1", sub)

	a.bindings.TurnPresentation.CompleteTurnItem(context.Background(), "thread-1", "turn-1", "item-1", turnitem.NewProtocolItemWithID("item-1", map[string]any{
		"type": "agent_message",
		"text": "legacy final",
	}))

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply cards after phase-less agent message = %d, want 1", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); !strings.Contains(got, "反馈中") {
		t.Fatalf("phase-less agent message title = %q, want feedback before completion", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "legacy final") {
		t.Fatalf("phase-less agent message body = %q, want final text", body)
	}

	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected no extra final card on completion, got %d cards", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("expected completion to patch feedback card into final, got %d patches", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); !strings.Contains(got, "最终答复") {
		t.Fatalf("completion fallback title = %q, want final answer", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "legacy final") {
		t.Fatalf("completion fallback body = %q, want final text", body)
	}
}

func TestCommentaryOnlyTurnPromotesLastAgentMessageToFinalOnCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.runtimeOwner.TurnBindings.BindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.runtimeOwner.TurnBindings.MarkTurnStartedAt("turn-1", time.Now())
	a.bindings.TurnPresentation.NoteTurnStarted("sess-1", sub)

	a.bindings.TurnPresentation.CompleteTurnItem(context.Background(), "thread-1", "turn-1", "item-1", turnitem.NewProtocolItemWithID("item-1", map[string]any{
		"type":  "agent_message",
		"text":  "visible commentary",
		"phase": "commentary",
	}))
	a.bindings.TurnPresentation.CompleteTurnItem(context.Background(), "thread-1", "turn-1", "item-2", turnitem.NewProtocolItemWithID("item-2", map[string]any{
		"type":  "agent_message",
		"text":  "",
		"phase": "final_answer",
	}))

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply cards after commentary = %d, want 1", len(ff.replyCards))
	}
	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "completed")
	if len(ff.replyCards) != 1 {
		t.Fatalf("expected no extra final card, got %d cards", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("expected commentary card to be patched into final, got %d patches", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); !strings.Contains(got, "最终答复") {
		t.Fatalf("completion fallback title = %q, want final answer", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "visible commentary") || strings.Contains(body, "任务已结束") {
		t.Fatalf("completion fallback body = %q, want commentary as final", body)
	}
}

func TestFinishTurnFailedAutoRetrySuppressesTerminalStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }
	a.bindings.AutoRetry.AutoRetryTracker().After = func(time.Duration, func()) appautoretry.DelayedTask {
		return &fakeDelayedTask{}
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	markSessionThreadLive(a, "sess-1", "thread-1")

	dispatchCodexNotification(a, "error", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","error":{"message":"upstream rejected","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":501}},"additionalDetails":"Not Implemented"}}`))
	if len(ff.replyCards) != 0 {
		t.Fatal("error notification must not start retry before completion")
	}
	finishTurn(a.bindings.Turns, "thread-1", "turn-1", "failed")

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply cards = %d, want 1 auto-retry card only", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != "Codex 自动重试" {
		t.Fatalf("reply card title = %q, want Codex 自动重试", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !containsAll(body, "自动发送“继续”", "upstream rejected", "501", "Not Implemented") {
		t.Fatalf("auto retry card body = %q", body)
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched cards = %d, want 0", len(ff.patchedCards))
	}
}

func TestBindTurnSubmissionRebindClearsMetadataState(t *testing.T) {
	a, _, _ := newTestApp(t)
	startedAt := time.Now().Add(-2 * time.Second)

	a.runtimeOwner.TurnBindings.BindTurnSubmission("thread-old", "turn-1", "sess-old", "sub-old")
	a.runtimeOwner.TurnBindings.MarkTurnStartedAt("turn-1", startedAt)
	a.runtimeOwner.TurnBindings.RecordTurnTokenUsage("thread-old", "turn-1", codexadapter.ThreadUsage(codexrpc.ThreadTokenUsage{
		Last: codexrpc.TokenUsageBreakdown{
			InputTokens: 42,
		},
	}))

	a.runtimeOwner.TurnBindings.BindTurnSubmission("thread-new", "turn-1", "sess-new", "sub-new")

	if usageLine, contextLine, elapsedLine := a.bindings.TurnMetadata.Metadata("turn-1", time.Now()); usageLine != "" || contextLine != "" || elapsedLine != "" {
		t.Fatalf("turnFinalMetadata() after rebind = %q, %q, %q, want all empty", usageLine, contextLine, elapsedLine)
	}
}
