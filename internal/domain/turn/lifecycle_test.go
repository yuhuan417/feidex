package turn

import (
	"testing"
	"time"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/submission"
)

func TestReviewResponseRemainsAuthority(t *testing.T) {
	sub := &submission.Submission{ID: "review", Kind: "review"}
	if CanBindPending("thread", "notification-turn", sub, false) {
		t.Fatal("unsolicited turn started consumed pending review")
	}
	if !CanBindPending("thread", "response-turn", sub, true) {
		t.Fatal("review response cannot bind its own turn")
	}
}

func TestCompletionFallbackCannotStealAnotherOperation(t *testing.T) {
	sess := &conversation.Session{ActiveOperations: []conversation.SessionActiveOperation{{Kind: conversation.OpKindSubmission, SubmissionID: "pending", ThreadID: "thread"}}}
	sub := &submission.Submission{ID: "pending"}
	if !CanBindCompletion(sess, sub, "thread", "turn") {
		t.Fatal("unbound operation could not bind completion")
	}
	for _, invalid := range []*submission.Submission{
		{ID: "other"}, {ID: "pending", Finalized: true}, {ID: "pending", TurnID: "older-turn"},
	} {
		if CanBindCompletion(sess, invalid, "thread", "turn") {
			t.Fatalf("completion stole operation for %+v", invalid)
		}
	}
	BindSession(sess, sub, "thread", "turn")
	if CanBindCompletion(sess, sub, "thread", "another-turn") {
		t.Fatal("already bound operation admitted another completion")
	}
}

func TestStartTimeAndCompletionStatus(t *testing.T) {
	binding := NewBinding(" thread ", " session ", " submission ")
	start := time.Unix(10, 0)
	binding.MarkStarted(start)
	binding.MarkStarted(start.Add(time.Second))
	if binding.ThreadID != "thread" || binding.SessionKey != "session" || binding.SubmissionID != "submission" || binding.StartedAt != start {
		t.Fatalf("duplicate start rewrote binding: %+v", binding)
	}
	for input, want := range map[string]submission.SubmissionStatus{"completed": submission.SubmissionStatusCompleted, "interrupted": submission.SubmissionStatusInterrupted, "failed": submission.SubmissionStatusFailed, "unknown": submission.SubmissionStatusFailed} {
		if CompletionStatus(input) != want {
			t.Fatalf("CompletionStatus(%q) = %q, want %q", input, CompletionStatus(input), want)
		}
	}
}
