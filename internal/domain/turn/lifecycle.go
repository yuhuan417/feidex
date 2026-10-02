// Package turn owns backend turn bindings and lifecycle decisions.
package turn

import (
	"strings"
	"time"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/submission"
)

// Binding is the owner of one backend turn. Usage and rendering metadata are
// kept outside this value by runtime/presentation.
type Binding struct {
	SessionKey   string
	SubmissionID string
	ThreadID     string
	StartedAt    time.Time
}

func NewBinding(threadID, sessionKey, submissionID string) Binding {
	return Binding{ThreadID: strings.TrimSpace(threadID), SessionKey: strings.TrimSpace(sessionKey), SubmissionID: strings.TrimSpace(submissionID)}
}

func (b *Binding) MarkStarted(at time.Time) {
	if b.StartedAt.IsZero() {
		b.StartedAt = at
	}
}

// CanBindPending preserves review/start's response turn as the authority for
// inline review; an unsolicited turn/started must not consume that pending
// submission unless the caller explicitly handles the review response.
func CanBindPending(threadID, turnID string, sub *submission.Submission, allowReview bool) bool {
	return strings.TrimSpace(threadID) != "" && strings.TrimSpace(turnID) != "" && sub != nil && (allowReview || strings.TrimSpace(sub.Kind) != "review")
}

// CanBindCompletion permits the missing-start fallback only for an unbound
// pending operation owned by the same submission. An older completed turn
// cannot steal the next submission on the same thread.
func CanBindCompletion(sess *conversation.Session, sub *submission.Submission, threadID, turnID string) bool {
	if sess == nil || sub == nil || sub.Finalized || strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" || strings.TrimSpace(sub.TurnID) != "" {
		return false
	}
	op := conversation.FindPendingSubmissionOperationByThread(sess, threadID)
	return op != nil && strings.TrimSpace(op.SubmissionID) == sub.ID && strings.TrimSpace(op.TurnID) == ""
}

// BindSession attaches the submission operation without changing its model
// snapshot, input, or reply anchors.
func BindSession(sess *conversation.Session, sub *submission.Submission, threadID, turnID string) {
	if sess == nil || sub == nil {
		return
	}
	conversation.UpsertActiveOperation(sess, conversation.SessionActiveOperation{Kind: conversation.OpKindSubmission, SubmissionID: sub.ID, ThreadID: threadID, TurnID: turnID})
	sess.Status = conversation.SessionStatusTurnInProgress.String()
	conversation.SetThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
}

func CompletionStatus(status string) submission.SubmissionStatus {
	switch status {
	case "completed":
		return submission.SubmissionStatusCompleted
	case "interrupted":
		return submission.SubmissionStatusInterrupted
	default:
		return submission.SubmissionStatusFailed
	}
}
