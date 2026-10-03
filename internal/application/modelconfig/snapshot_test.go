package modelconfig

import (
	"testing"

	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	"feidex/internal/domain/submission"
)

type sourceRepository struct{ bindingID string }

func (r *sourceRepository) ModelSourceRevision(sess *conversation.Session) SourceRevision {
	r.bindingID = sess.BindingID
	return SourceRevision{Session: sess, Binding: &routing.AgentBinding{ModelOverride: sess.BindingID}, Global: domain.GlobalValues{Model: "global"}}
}

func TestSubmissionSnapshotUsesSubmissionBindingWithoutChangingSession(t *testing.T) {
	repo := &sourceRepository{}
	service := SnapshotService{Repository: repo}
	sess := &conversation.Session{BindingID: "session-binding", ActiveTurnID: "turn"}
	snapshot := service.SubmissionSnapshot("codex", sess, &submission.Submission{BindingID: "submission-binding"})
	if snapshot.Model != "submission-binding" || repo.bindingID != "submission-binding" {
		t.Fatalf("snapshot = %+v, read binding=%s", snapshot, repo.bindingID)
	}
	if sess.BindingID != "session-binding" || sess.ActiveTurnID != "turn" {
		t.Fatalf("snapshot changed session: %+v", sess)
	}
	if service.SubmissionSnapshot("codex", sess, &submission.Submission{}).Model != "session-binding" {
		t.Fatal("empty submission binding lost session scope")
	}
}
