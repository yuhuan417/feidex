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

func TestClaudeRuntimeStreamsPartialTextByPatchingCurrentCard(t *testing.T) {
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

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "hello"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after first partial = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "hello") {
		t.Fatalf("first partial body = %q, want hello", body)
	}

	session.turns[1].LastRenderedAt = time.Now().Add(-claudePartialUpdateMinInterval)
	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "hello\n\nworld"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after patch = %d, want 1", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after second partial = %d, want 1", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "world") {
		t.Fatalf("patched partial body = %q, want world", body)
	}
}

func TestClaudeRuntimeToolBoundaryStartsNewCardWithoutRepeatingEarlierText(t *testing.T) {
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

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "before tool"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after first partial = %d, want 1", len(ff.replyCards))
	}

	runtime.handleToolComplete(session, claudecli.ToolCompleteEvent{
		TurnNumber: 1,
		ID:         "tool-1",
		Name:       "Read",
		Input:      map[string]any{"file_path": "/tmp/demo.txt"},
	})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after tool boundary = %d, want 2", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "Read") {
		t.Fatalf("tool card body = %q, want tool name", body)
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "before tool\n\nafter tool"})
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count after second segment = %d, want 3", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[2])
	if !strings.Contains(body, "after tool") {
		t.Fatalf("second segment body = %q, want after tool", body)
	}
	if strings.Contains(body, "before tool") {
		t.Fatalf("second segment body repeated earlier text: %q", body)
	}
}

func TestClaudeRuntimeTurnCompletePromotesLastStreamingCardToFinal(t *testing.T) {
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

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "final answer"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count before completion = %d, want 1", len(ff.replyCards))
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after completion = %d, want 1 reused card", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after completion = %d, want 1 final patch", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.patchedCards[0]); got != "最终答复" {
		t.Fatalf("final card title = %q, want 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "final answer") {
		t.Fatalf("final card body = %q, want final answer", body)
	}
}

func TestClaudeRuntimePlanBoundaryClosesCurrentStreamingSegment(t *testing.T) {
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

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "before plan"})
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

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "before plan\n\nafter plan"})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after post-plan text = %d, want 2", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[1])
	if !strings.Contains(body, "after plan") {
		t.Fatalf("post-plan segment body = %q, want after plan", body)
	}
	if strings.Contains(body, "before plan") {
		t.Fatalf("post-plan segment body repeated earlier text: %q", body)
	}
}
