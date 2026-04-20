package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/config"
)

type blockingReplyCardClient struct {
	*fakeFeishuClient
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingReplyCardClient) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	c.once.Do(func() {
		close(c.entered)
		<-c.release
	})
	return c.fakeFeishuClient.ReplyCard(ctx, messageID, card, inThread)
}

func TestClaudeRuntimeCachesPartialTextUntilBoundary(t *testing.T) {
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
	if len(ff.replyCards) != 0 {
		t.Fatalf("reply card count after first partial = %d, want 0", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after first partial = %d, want 0", len(ff.patchedCards))
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "hello\n\nworld"})
	if len(ff.replyCards) != 0 {
		t.Fatalf("reply card count after second partial = %d, want 0", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after second partial = %d, want 0", len(ff.patchedCards))
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
	if len(ff.replyCards) != 0 {
		t.Fatalf("reply card count after first partial = %d, want 0", len(ff.replyCards))
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
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after second segment text = %d, want 2", len(ff.replyCards))
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true})
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply card count after turn completion = %d, want 3", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[2])
	if !strings.Contains(body, "after tool") {
		t.Fatalf("final segment body = %q, want after tool", body)
	}
	if strings.Contains(body, "before tool") {
		t.Fatalf("final segment body repeated earlier text: %q", body)
	}
	if got := cardHeaderTitle(t, ff.replyCards[2]); got != "最终答复" {
		t.Fatalf("final card title = %q, want 最终答复", got)
	}
}

func TestClaudeRuntimeTurnCompleteSendsFinalCardWithoutPatch(t *testing.T) {
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
	if len(ff.replyCards) != 0 {
		t.Fatalf("reply card count before completion = %d, want 0", len(ff.replyCards))
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after completion = %d, want 1", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count after completion = %d, want 0", len(ff.patchedCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != "最终答复" {
		t.Fatalf("final card title = %q, want 最终答复", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "final answer") {
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
	if len(ff.replyCards) != 0 {
		t.Fatalf("reply card count before plan = %d, want 0", len(ff.replyCards))
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := runtime.handleExitPlanMode(ctx, session, claudecli.PlanInfo{Plan: "1. inspect\n2. implement"})
		errCh <- err
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for (len(ff.sendCards) == 0 || len(ff.replyCards) == 0) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.sendCards) != 1 {
		t.Fatalf("plan confirmation card count = %d, want 1", len(ff.sendCards))
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count before plan confirmation = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "before plan") {
		t.Fatalf("pre-plan segment body = %q, want before plan", body)
	}
	if got := cardHeaderTitle(t, ff.sendCards[0]); got != "Claude 计划确认" {
		t.Fatalf("plan confirmation title = %q, want Claude 计划确认", got)
	}

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("handleExitPlanMode() error = %v, want context canceled", err)
	}

	runtime.handleTextEvent(session, claudecli.TextEvent{TurnNumber: 1, FullText: "before plan\n\nafter plan"})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after post-plan text = %d, want 1", len(ff.replyCards))
	}

	runtime.handleTurnComplete(session, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true})
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after turn completion = %d, want 2", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[1])
	if !strings.Contains(body, "after plan") {
		t.Fatalf("final post-plan body = %q, want after plan", body)
	}
	if strings.Contains(body, "before plan") {
		t.Fatalf("final post-plan body repeated earlier text: %q", body)
	}
}

func TestClaudeRuntimeConcurrentFlushesOnlySendOneReply(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	base := &fakeFeishuClient{}
	blocking := &blockingReplyCardClient{
		fakeFeishuClient: base,
		entered:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	a.feishu = blocking

	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)

	runtime := &claudeRuntime{app: a, pending: map[string]*claudePendingInteraction{}}
	session := &claudeSessionState{
		sessionID: "thread-1",
		turns: map[int]*claudeTurnState{
			1: {TurnNumber: 1, TurnID: "turn-1", FullText: "duplicate me"},
		},
	}

	doneCh := make(chan struct{})
	go func() {
		runtime.flushAndCloseClaudeTurnSegment(session, 1)
		close(doneCh)
	}()

	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("first flush did not reach ReplyCard")
	}

	runtime.flushAndCloseClaudeTurnSegment(session, 1)
	close(blocking.release)

	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("first flush did not complete")
	}

	if len(base.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(base.replyCards))
	}
	if body := cardMarkdownContent(t, base.replyCards[0]); !strings.Contains(body, "duplicate me") {
		t.Fatalf("reply card body = %q, want duplicate text", body)
	}
}
