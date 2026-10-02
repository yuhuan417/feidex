package maintenance

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/state"
	"strings"
)

type StateProvider interface {
	// PendingRequests returns all pending requests in the store.
	PendingRequests() []*state.PendingRequest
	// UpdatePending applies a mutation to a pending request by ID.
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
	// DeleteMessageLinks removes message links matching the predicate.
	DeleteMessageLinks(match func(*state.MessageLink) bool)
	// DeletePendingRequests removes pending requests matching the predicate.
	DeletePendingRequests(match func(*state.PendingRequest) bool)
	// DeleteSubmission removes a submission by ID.
	DeleteSubmission(id string)
	// Sessions returns all sessions in the store.
	Sessions() []*conversation.Session
	// SaveSession persists a session snapshot.
	SaveSession(sess *conversation.Session) error
}

// RuntimeStateProvider narrows runtime state access to the methods used by
// the service for turn binding and item state cleanup.
type RuntimeStateProvider interface {
	// ClearTurnBinding removes the turn binding for the given turn ID.
	ClearTurnBinding(turnID string)
	// ClearTurnItemStates removes all turn item states for the given turn ID.
	ClearTurnItemStates(turnID string)
	// ClearPendingTurnBindingForSubmission removes pending turn bindings for the
	// given thread/submission pair.
	ClearPendingTurnBindingForSubmission(threadID, submissionID string)
}

// SubmissionCleanup is the single owner of turn-associated runtime cleanup.
// Open async questions and Claude control requests retain their reply anchors.
type SubmissionCleanup struct {
	Repository StateProvider
	Runtime    RuntimeStateProvider
}

func (s SubmissionCleanup) CleanupSubmissionRuntimeState(sub *domainsubmission.Submission) {
	if sub == nil {
		return
	}
	stateProvider := s.Repository
	runtimeProvider := s.Runtime
	if stateProvider == nil || runtimeProvider == nil {
		return
	}
	submissionID := strings.TrimSpace(sub.ID)
	turnID := strings.TrimSpace(sub.TurnID)
	threadID := strings.TrimSpace(sub.ThreadID)
	// Async questions and Claude interactive requests can be answered after
	// their producing turn completes. Keep their local form and reply anchor
	// while clearing turn runtime state.
	survivingRequests := map[string]bool{}
	survivingMessages := map[string]bool{}
	for _, req := range stateProvider.PendingRequests() {
		if req != nil && req.TurnID == turnID && interaction.OutlivesTurn(req.Backend, req.Kind, req.Status) {
			survivingRequests[req.ID] = true
			survivingMessages[req.FeishuMsgID] = true
		}
	}
	stateProvider.DeleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil || survivingMessages[link.MessageID] {
			return false
		}
		if submissionID != "" && strings.TrimSpace(link.SubmissionID) == submissionID {
			return true
		}
		if turnID != "" && strings.TrimSpace(link.TurnID) == turnID {
			return true
		}
		return false
	})
	if turnID != "" {
		stateProvider.DeletePendingRequests(func(req *state.PendingRequest) bool {
			return req != nil && strings.TrimSpace(req.TurnID) == turnID && !survivingRequests[req.ID]
		})
	}
	if submissionID != "" {
		stateProvider.DeleteSubmission(submissionID)
	}
	if turnID != "" {
		runtimeProvider.ClearTurnBinding(turnID)
		runtimeProvider.ClearTurnItemStates(turnID)
	}
	if submissionID != "" && threadID != "" {
		runtimeProvider.ClearPendingTurnBindingForSubmission(threadID, submissionID)
	}
}
