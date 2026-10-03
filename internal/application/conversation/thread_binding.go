package conversation

import (
	"strings"

	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
)

type TurnLookup interface {
	FindSubmissionByTurn(string, string) (string, *domainsubmission.Submission)
}
type TurnBindings interface{ RebindTurnThreadID(string, string) }
type ReplyBindings interface {
	RecordSubmissionSourceLinks(*domainsubmission.Submission)
	RecordRootTurnBinding(string, string, string, string)
}
type ThreadBindingDependencies struct {
	Lookup   TurnLookup
	Bindings TurnBindings
	Replies  ReplyBindings
}

// BindBackendSessionThread applies the backend's acknowledged conversation ID
// to all matching operations before publishing reply and live-thread bindings.
func (s *Service) BindBackendSessionThread(sessionKey, turnID, threadID, name string) error {
	sessionKey, turnID, threadID = strings.TrimSpace(sessionKey), strings.TrimSpace(turnID), strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return nil
	}
	d := s.Deps.ThreadBinding
	repo := s.Deps.Operations
	var workspaceID string
	bindSubmission := func(id, turnID string) error {
		if err := repo.UpdateSubmission(id, func(value *domainsubmission.Submission) {
			value.ThreadID = threadID
			if strings.TrimSpace(value.TurnID) == "" {
				value.TurnID = turnID
			}
		}); err != nil {
			return err
		}
		d.Bindings.RebindTurnThreadID(turnID, threadID)
		if updated := repo.Submission(id); updated != nil {
			workspaceID = textutil.FirstNonEmpty(workspaceID, strings.TrimSpace(updated.WorkspaceID))
			d.Replies.RecordSubmissionSourceLinks(updated)
		}
		return nil
	}
	if turnID != "" {
		if _, sub := d.Lookup.FindSubmissionByTurn("", turnID); sub != nil {
			workspaceID = strings.TrimSpace(sub.WorkspaceID)
			if err := bindSubmission(sub.ID, turnID); err != nil {
				return err
			}
		}
	}
	sess := s.Deps.Repository.Session(sessionKey)
	if sess == nil {
		return nil
	}
	workspaceID = textutil.FirstNonEmpty(workspaceID, strings.TrimSpace(sess.WorkspaceID))
	conversation.EnsureActiveOperations(sess)
	var operations []conversation.SessionActiveOperation
	for _, op := range sess.ActiveOperations {
		if strings.TrimSpace(op.TurnID) == "" || turnID != "" && strings.TrimSpace(op.TurnID) != turnID {
			continue
		}
		operations = append(operations, op)
		if id := strings.TrimSpace(op.SubmissionID); id != "" {
			if err := bindSubmission(id, strings.TrimSpace(op.TurnID)); err != nil {
				return err
			}
		}
	}
	updated, err := repo.UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		conversation.EnsureActiveOperations(current)
		for _, op := range operations {
			conversation.UpsertActiveOperation(current, conversation.SessionActiveOperation{
				Kind:         textutil.FirstNonEmpty(strings.TrimSpace(op.Kind), conversation.OpKindSubmission),
				SubmissionID: strings.TrimSpace(op.SubmissionID), ThreadID: threadID, TurnID: strings.TrimSpace(op.TurnID),
			})
		}
		conversation.SetThreadContext(current, workspaceID, threadID, current.ActiveThreadName, current.ActiveThreadPreview)
		if strings.TrimSpace(current.ActiveThreadName) == "" {
			current.ActiveThreadName = name
		}
	})
	if err != nil {
		return err
	}
	s.Deps.Live.MarkSessionThreadLive(sessionKey, threadID)
	if updated != nil && strings.TrimSpace(updated.RootMessageID) != "" && turnID != "" {
		d.Replies.RecordRootTurnBinding(updated.RootMessageID, sessionKey, threadID, turnID)
	}
	return nil
}
