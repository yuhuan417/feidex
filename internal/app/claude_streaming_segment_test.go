package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/config"
)

func TestClaudeRuntimeAssistantTextRepliesImmediately(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "hello"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after first assistant message = %d, want 1", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after first assistant message = %d, want 0", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "hello") {
		t.Fatalf("first assistant card body = %q, want hello", body)
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "world"})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after second assistant message = %d, want 2", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after second assistant message = %d, want 0", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "world") {
		t.Fatalf("second assistant card body = %q, want world", body)
	}
}

func TestClaudeRuntimeToolBoundaryKeepsLaterAssistantTextIntact(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	first := "I need to split the changes. Let me first commit just the blur fix, then the tooltip."
	second := "I'll temporarily revert the tooltip changes, commit the blur fix, then re-apply and commit the tooltip."

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: first})
	runtime.handleToolComplete(session, claudecli.ToolCompleteEvent{
		TurnNumber: 1,
		ID:         "tool-1",
		Name:       "Read",
		Input:      map[string]any{"file_path": "/tmp/demo.txt"},
	})
	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: second})

	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count before completion = %d, want 3", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, first) {
		t.Fatalf("first assistant card body = %q, want first message", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "Read") {
		t.Fatalf("tool card body = %q, want tool name", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[2]); !strings.HasPrefix(body, "I'll temporarily") {
		t.Fatalf("second assistant card lost leading chars: %q", body)
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true, Result: second})
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count after completion = %d, want no duplicate final card", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after completion = %d, want 1 final patch", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "最终答复" {
		t.Fatalf("patched final title = %q, want 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, second) {
		t.Fatalf("patched final body = %q, want second message", body)
	}
}

func TestClaudeRuntimeAssistantTextStartsNewQuietWorkingCardBoundary(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleToolComplete(session, claudecli.ToolCompleteEvent{
		TurnNumber: 1,
		ID:         "tool-1",
		Name:       "Read",
		Input: map[string]any{
			"file_path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
		},
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after first tool = %d, want 1", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != quietWorkingCardTitle {
		t.Fatalf("first working card title = %q, want %q", got, quietWorkingCardTitle)
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "first reply"})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after assistant text = %d, want 2", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after assistant text = %d, want 0", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "first reply") {
		t.Fatalf("assistant reply body = %q, want first reply", body)
	}

	runtime.handleToolComplete(session, claudecli.ToolCompleteEvent{
		TurnNumber: 1,
		ID:         "tool-2",
		Name:       "TaskUpdate",
		Input: map[string]any{
			"taskId": "7",
			"status": "in_progress",
		},
	})
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count after second tool = %d, want 3 for a new working card", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after second tool = %d, want 0 because the old working card should be closed", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[2]); got != quietWorkingCardTitle {
		t.Fatalf("second working card title = %q, want %q", got, quietWorkingCardTitle)
	}
	if body := cardMarkdownContent(t, ff.replyCards[2]); !strings.Contains(body, "Update task `7` -> `in_progress`") {
		t.Fatalf("second working card body = %q", body)
	}
}

func TestClaudeRuntimeThinkingUsesProgressWorkingCardAndReusesItForAssistantText(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleThinkingEvent(session, claudecli.ThinkingEvent{
		TurnNumber:   1,
		Thinking:     "private chain of thought",
		FullThinking: "private chain of thought",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after thinking = %d, want 1", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != quietWorkingCardTitle {
		t.Fatalf("thinking working card title = %q, want %q", got, quietWorkingCardTitle)
	}
	thinkingBody := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(thinkingBody, "思考中...") {
		t.Fatalf("thinking working card body = %q, want 思考中...", thinkingBody)
	}
	if strings.Contains(thinkingBody, "private chain of thought") {
		t.Fatalf("thinking working card should not expose raw reasoning: %q", thinkingBody)
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "visible answer"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after assistant text = %d, want 1 because the thinking card should be reused", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after assistant text = %d, want 1", len(ff.patchedCards))
	}
	patchedBody := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(patchedBody, "visible answer") {
		t.Fatalf("patched assistant body = %q, want visible answer", patchedBody)
	}
	if strings.Contains(patchedBody, "思考中...") {
		t.Fatalf("patched assistant body should remove thinking placeholder: %q", patchedBody)
	}
}

func TestClaudeRuntimeThinkingRemainsHiddenOutsideProgress(t *testing.T) {
	modes := []config.QuietMode{
		config.QuietModeVerbose,
		config.QuietModeNormal,
		config.QuietModeFinal,
	}
	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			a, ff, _ := newTestApp(t)
			a.cfg.Feishu.Quiet = mode
			sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
			a.noteTurnStarted("sess-1", sub)

			runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
			session := &claudeSessionState{
				sessionID: "thread-1",
				turns: map[int]*claudeTurnState{
					1: {TurnNumber: 1, TurnID: "turn-1"},
				},
			}

			runtime.handleThinkingEvent(session, claudecli.ThinkingEvent{
				TurnNumber:   1,
				Thinking:     "private chain of thought",
				FullThinking: "private chain of thought",
			})
			if len(ff.replyCards) != 0 || len(ff.patchedCards) != 0 {
				t.Fatalf("thinking should stay hidden in %s, replies=%d patches=%d", mode, len(ff.replyCards), len(ff.patchedCards))
			}
		})
	}
}

func TestClaudeRuntimeTurnCompleteUsesResultFallbackWithoutAssistantText(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{
		TurnNumber: 1,
		Success:    true,
		Result:     "final answer",
	})

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after result fallback = %d, want 1", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after result fallback = %d, want 0", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != "最终答复" {
		t.Fatalf("final fallback title = %q, want 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "final answer") {
		t.Fatalf("final fallback body = %q, want final answer", body)
	}
}

func TestClaudeRuntimeTurnCompletePatchesContextUsageAsynchronously(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)
	a.bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.markTurnStartedAt("turn-1", time.Now().Add(-1500*time.Millisecond))

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	session := &claudeSessionState{
		sessionID: "thread-1",
		getContextUsage: func(context.Context) (claudecli.ContextUsage, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return claudecli.ContextUsage{Percentage: 13}, nil
		},
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{
		TurnNumber: 1,
		Success:    true,
		Result:     "final answer",
	})

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after final fallback = %d, want 1", len(ff.replyCards))
	}
	initialFooter := cardFooterTextForTest(ff.replyCards[0])
	if !strings.Contains(initialFooter, "context used: calculating...") {
		t.Fatalf("initial footer should include pending context usage: %q", initialFooter)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected async context usage fetch to start")
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for len(ff.patchedCards) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected final card to be patched with context usage")
	}
	lastFooter := cardFooterTextForTest(ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(lastFooter, "context used: 13.0%") {
		t.Fatalf("patched footer = %q", lastFooter)
	}
	if !strings.Contains(lastFooter, "elapsed:") {
		t.Fatalf("patched footer should keep elapsed: %q", lastFooter)
	}
}

func TestClaudeRuntimeTurnCompleteReusesThinkingCardForFinalFallback(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleThinkingEvent(session, claudecli.ThinkingEvent{
		TurnNumber:   1,
		Thinking:     "private chain of thought",
		FullThinking: "private chain of thought",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after thinking = %d, want 1", len(ff.replyCards))
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{
		TurnNumber: 1,
		Success:    true,
		Result:     "final answer",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after final fallback = %d, want 1 because the thinking card should be reused", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after final fallback = %d, want 1", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "最终答复" {
		t.Fatalf("patched final title = %q, want 最终答复", got)
	}
	patchedBody := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(patchedBody, "final answer") {
		t.Fatalf("patched final body = %q, want final answer", patchedBody)
	}
	if strings.Contains(patchedBody, "思考中...") {
		t.Fatalf("patched final body should remove thinking placeholder: %q", patchedBody)
	}
}

func TestClaudeRuntimeTurnCompleteReusesLatestThinkingCardAfterAssistantText(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "draft answer"})
	runtime.handleThinkingEvent(session, claudecli.ThinkingEvent{
		TurnNumber:   1,
		Thinking:     "private chain of thought",
		FullThinking: "private chain of thought",
	})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count before completion = %d, want 2", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[1]); got != quietWorkingCardTitle {
		t.Fatalf("thinking card title = %q, want %q", got, quietWorkingCardTitle)
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{
		TurnNumber: 1,
		Success:    true,
		Result:     "draft answer",
	})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after completion = %d, want 2 because the latest thinking card should be reused", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after completion = %d, want 1", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "最终答复" {
		t.Fatalf("patched final title = %q, want 最终答复", got)
	}
	patchedBody := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(patchedBody, "draft answer") {
		t.Fatalf("patched final body = %q, want draft answer", patchedBody)
	}
	if strings.Contains(patchedBody, "思考中...") {
		t.Fatalf("patched final body should remove thinking placeholder: %q", patchedBody)
	}
}

func TestClaudeRuntimePlanModeDoesNotDelayAssistantMessages(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID:         "thread-1",
		workspaceID:       a.cfg.Workspaces[0].ID,
		currentTurnNumber: 1,
		startedAt:         time.Now(),
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "before plan"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count before plan = %d, want 1", len(ff.replyCards))
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := runtime.handleExitPlanMode(ctx, session, claudecli.PlanInfo{Plan: "1. inspect\n2. implement"})
		errCh <- err
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for len(ff.sendCards) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.sendCards) != 1 {
		t.Fatalf("plan confirmation card count = %d, want 1", len(ff.sendCards))
	}
	if got := cardHeaderTitle(t, ff.sendCards[0]); got != "Claude 计划确认" {
		t.Fatalf("plan confirmation title = %q, want Claude 计划确认", got)
	}

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("handleExitPlanMode() error = %v, want context canceled", err)
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "after plan"})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after second assistant message = %d, want 2", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "after plan") {
		t.Fatalf("post-plan assistant card body = %q, want after plan", body)
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true, Result: "after plan"})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after completion = %d, want no duplicate final card", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after completion = %d, want 1 final patch", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "最终答复" {
		t.Fatalf("patched final title = %q, want 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "after plan") {
		t.Fatalf("patched final body = %q, want after plan", body)
	}
}

func TestClaudeRuntimeQuietFinalSuppressesIntermediateTextButStillDeliversFinalAnswer(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeFinal
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1"},
		},
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, Text: "intermediate answer"})
	if len(ff.replyCards) != 0 || len(ff.patchedCards) != 0 {
		t.Fatalf("quiet final should suppress intermediate text, replies=%d patches=%d", len(ff.replyCards), len(ff.patchedCards))
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true, Result: "final answer"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after final completion = %d, want 1", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after final completion = %d, want 0", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != "最终答复" {
		t.Fatalf("final reply title = %q, want 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "final answer") {
		t.Fatalf("final reply body = %q, want final answer", body)
	}
}
