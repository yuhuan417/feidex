package conversation

import "testing"

func TestQueueOrderDeduplicationAndActiveBoundary(t *testing.T) {
	sess := &Session{}
	Enqueue(sess, "first")
	Enqueue(sess, "second")
	Enqueue(sess, "first")
	RefreshPendingStatus(sess)
	if !ShouldStartNextSubmission(sess) || sess.Status != SessionStatusQueued.String() || len(sess.Queue) != 2 {
		t.Fatalf("invalid pending queue: %+v", sess)
	}
	UpsertActiveOperation(sess, SessionActiveOperation{SubmissionID: Dequeue(sess), TurnID: "turn-1"})
	sess.Status = SessionStatusTurnInProgress.String()
	RefreshPendingStatus(sess)
	if ShouldStartNextSubmission(sess) || sess.Status != SessionStatusTurnInProgress.String() {
		t.Fatalf("queue admitted work across active turn: %+v", sess)
	}
	RemoveActiveOperation(sess, "first", "turn-1")
	if Dequeue(sess) != "second" || Dequeue(sess) != "" {
		t.Fatal("queue did not preserve order")
	}
	RefreshPendingStatus(sess)
	if sess.Status != SessionStatusIdle.String() {
		t.Fatalf("drained queue not idle: %+v", sess)
	}
}
