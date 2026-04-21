package app

import (
	"context"
	"errors"
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
}
