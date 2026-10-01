package app

import (
	"context"
	"errors"
	"feidex/internal/app/turn"
	"testing"
)

func TestExecuteQuietWorkingCardOp(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).turnStreamTracker().Streams["turn-1"] = &turnStream{TurnID: "turn-1", QuietWorking: &turn.QuietWorkingCard{}}
	executeQuietWorkingCardOp(a, context.Background(), sub, turn.QuietWorkingCardOp{
		TurnID: "turn-1",
		Body:   "Read `quiet_mode.go`",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	if got := newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking; got == nil || got.MessageID == "" || got.RenderedBody != "Read `quiet_mode.go`" {
		t.Fatalf("commitQuietWorkingCardRender(reply) = %+v", got)
	}

	newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking = &turn.QuietWorkingCard{MessageID: "reply-card-id", RenderedBody: "before"}
	executeQuietWorkingCardOp(a, context.Background(), sub, turn.QuietWorkingCardOp{
		TurnID:    "turn-1",
		MessageID: "reply-card-id",
		Body:      "Update `quiet_mode.go`",
	})
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards = %d, want 1", len(ff.patchedCards))
	}
	if got := newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking.RenderedBody; got != "Update `quiet_mode.go`" {
		t.Fatalf("commitQuietWorkingCardRender(patch) = %q", got)
	}

	ff.patchCardErr = errors.New("boom")
	newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking = &turn.QuietWorkingCard{MessageID: "reply-card-id", RenderedBody: "stable"}
	executeQuietWorkingCardOp(a, context.Background(), sub, turn.QuietWorkingCardOp{
		TurnID:    "turn-1",
		MessageID: "reply-card-id",
		Body:      "after error",
	})
	if got := newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking.RenderedBody; got != "stable" {
		t.Fatalf("patch error should not commit render, got %q", got)
	}
}
