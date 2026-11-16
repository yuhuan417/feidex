package conversation

import (
	domainconversation "feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	"testing"
)

type startupRepository struct {
	sessions    []*domainconversation.Session
	submissions map[string]*domainsubmission.Submission
}

func (r *startupRepository) Sessions() []*domainconversation.Session       { return r.sessions }
func (r *startupRepository) SaveSession(*domainconversation.Session) error { return nil }
func (r *startupRepository) Submission(id string) *domainsubmission.Submission {
	return r.submissions[id]
}
func (r *startupRepository) FinalizeSubmission(id, status string) error {
	r.submissions[id].Finalize(status)
	return nil
}
func (*startupRepository) PendingRequests() []*interaction.PendingRequest { return nil }
func (*startupRepository) UpdatePending(string, func(*interaction.PendingRequest)) error {
	return nil
}

func TestStartupResetInterruptsPersistedBackgroundWorkflow(t *testing.T) {
	sub := &domainsubmission.Submission{ID: "sub-1", Status: domainsubmission.SubmissionStatusWaitingBackground.String()}
	repo := &startupRepository{
		sessions: []*domainconversation.Session{{
			Key: "session-1", Status: domainconversation.SessionStatusWaitingBackground.String(),
			ActiveOperations: []domainconversation.SessionActiveOperation{{
				Kind: domainconversation.OpKindBackgroundWorkflow, SubmissionID: "sub-1", ThreadID: "thread-1",
			}},
		}},
		submissions: map[string]*domainsubmission.Submission{"sub-1": sub},
	}
	if err := (StartupState{Repository: repo, DefaultWorkspaceID: func() string { return "workspace-1" }}).Reset(); err != nil {
		t.Fatal(err)
	}
	if !sub.Finalized || sub.Status != domainsubmission.SubmissionStatusInterrupted.String() {
		t.Fatalf("submission after startup recovery = %+v, want interrupted", sub)
	}
	if sess := repo.sessions[0]; sess.Status != domainconversation.SessionStatusIdle.String() || len(sess.ActiveOperations) != 0 {
		t.Fatalf("session after startup recovery = %+v, want idle", sess)
	}
}
