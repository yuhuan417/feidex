package app

import (
	"context"
	"testing"

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
		s.OutputText = "non-final output"
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

func TestDuplicateFinalAnswerIsDroppedBeforeTurnCompleted(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
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

	if len(ff.replyCards) != 1 {
		t.Fatalf("expected only first final card to be sent, got %d", len(ff.replyCards))
	}
}
