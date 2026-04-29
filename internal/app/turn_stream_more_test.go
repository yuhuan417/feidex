package app

import (
	"context"
	"testing"
)

func TestTurnStreamLifecycleDeliversItemCardsWithoutStoringAccumulation(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	newTurnStreamService(a).updatePendingPlan("turn-1", "- [in_progress] run")
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "reason-1", map[string]any{
		"type":    "reasoning",
		"summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}},
	})

	updated := a.store.GetSubmission(sub.ID)
	if updated == nil {
		t.Fatalf("submission after completeTurnItem = %+v", updated)
	}

	result := newTurnStreamService(a).flushTurnStream(context.Background(), "thread-1", "turn-1")
	updated = a.store.GetSubmission(sub.ID)
	if result.SawFinal || updated == nil {
		t.Fatalf("flushTurnStream() = %+v, submission=%+v", result, updated)
	}
	if newTurnStreamService(a).turnStreamTracker().Streams["turn-1"] != nil {
		t.Fatal("expected turn stream to be cleared after flush")
	}
	if len(ff.replyCards) < 2 {
		t.Fatalf("reply cards = %d, want plan + item cards", len(ff.replyCards))
	}
}
