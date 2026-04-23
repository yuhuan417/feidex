package app

import (
	"strings"
	"time"

	"feidex/internal/state"
)

const (
	sessionOpKindSubmission = "submission"
	sessionOpKindTurn       = "turn"
)

func sessionEnsureActiveOperations(sess *state.Session) {
	if sess == nil || len(sess.ActiveOperations) > 0 {
		return
	}
	if strings.TrimSpace(sess.ActiveTurnID) == "" && strings.TrimSpace(sess.ActiveSubmissionID) == "" {
		return
	}
	kind := sessionOpKindTurn
	if strings.TrimSpace(sess.ActiveSubmissionID) != "" {
		kind = sessionOpKindSubmission
	}
	sess.ActiveOperations = append(sess.ActiveOperations, state.SessionActiveOperation{
		Kind:         kind,
		SubmissionID: strings.TrimSpace(sess.ActiveSubmissionID),
		ThreadID:     strings.TrimSpace(sess.ActiveThreadID),
		TurnID:       strings.TrimSpace(sess.ActiveTurnID),
	})
}

func sessionResetActiveOperations(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.ActiveOperations = nil
	sessionSyncLegacyActiveFields(sess)
}

func sessionSyncLegacyActiveFields(sess *state.Session) {
	if sess == nil {
		return
	}
	if len(sess.ActiveOperations) == 0 {
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		return
	}
	foreground := sess.ActiveOperations[len(sess.ActiveOperations)-1]
	sess.ActiveTurnID = strings.TrimSpace(foreground.TurnID)
	sess.ActiveSubmissionID = strings.TrimSpace(foreground.SubmissionID)
	if strings.TrimSpace(foreground.ThreadID) != "" {
		sess.ActiveThreadID = strings.TrimSpace(foreground.ThreadID)
	}
}

func sessionForegroundOperation(sess *state.Session) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	sessionEnsureActiveOperations(sess)
	if len(sess.ActiveOperations) == 0 {
		return nil
	}
	op := sess.ActiveOperations[len(sess.ActiveOperations)-1]
	return &op
}

func sessionHasActiveOperations(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	sessionEnsureActiveOperations(sess)
	return len(sess.ActiveOperations) > 0
}

func sessionUpsertActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	if sess == nil {
		return
	}
	sessionEnsureActiveOperations(sess)
	op.Kind = strings.TrimSpace(op.Kind)
	op.SubmissionID = strings.TrimSpace(op.SubmissionID)
	op.ThreadID = strings.TrimSpace(op.ThreadID)
	op.TurnID = strings.TrimSpace(op.TurnID)
	if op.Kind == "" {
		if op.SubmissionID != "" {
			op.Kind = sessionOpKindSubmission
		} else {
			op.Kind = sessionOpKindTurn
		}
	}

	next := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations)+1)
	updated := false
	for i := range sess.ActiveOperations {
		candidate := sess.ActiveOperations[i]
		if sessionActiveOperationMatches(candidate, op.SubmissionID, op.TurnID) {
			candidate.Kind = firstNonEmpty(op.Kind, strings.TrimSpace(candidate.Kind))
			candidate.SubmissionID = firstNonEmpty(op.SubmissionID, strings.TrimSpace(candidate.SubmissionID))
			candidate.ThreadID = firstNonEmpty(op.ThreadID, strings.TrimSpace(candidate.ThreadID))
			candidate.TurnID = firstNonEmpty(op.TurnID, strings.TrimSpace(candidate.TurnID))
			if op.StartedAt != 0 {
				candidate.StartedAt = op.StartedAt
			}
			next = append(next, candidate)
			updated = true
			continue
		}
		next = append(next, candidate)
	}
	if !updated {
		if op.StartedAt == 0 {
			op.StartedAt = time.Now().Unix()
		}
		next = append(next, op)
	}
	sess.ActiveOperations = next
	sessionSyncLegacyActiveFields(sess)
}

func sessionPrependActiveOperation(sess *state.Session, op state.SessionActiveOperation) {
	if sess == nil {
		return
	}
	sessionEnsureActiveOperations(sess)
	op.Kind = strings.TrimSpace(op.Kind)
	op.SubmissionID = strings.TrimSpace(op.SubmissionID)
	op.ThreadID = strings.TrimSpace(op.ThreadID)
	op.TurnID = strings.TrimSpace(op.TurnID)
	if op.Kind == "" {
		if op.SubmissionID != "" {
			op.Kind = sessionOpKindSubmission
		} else {
			op.Kind = sessionOpKindTurn
		}
	}

	next := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations)+1)
	if op.StartedAt == 0 {
		op.StartedAt = time.Now().Unix()
	}
	next = append(next, op)
	for i := range sess.ActiveOperations {
		candidate := sess.ActiveOperations[i]
		if sessionActiveOperationMatches(candidate, op.SubmissionID, op.TurnID) {
			next[0].Kind = firstNonEmpty(next[0].Kind, strings.TrimSpace(candidate.Kind))
			next[0].SubmissionID = firstNonEmpty(next[0].SubmissionID, strings.TrimSpace(candidate.SubmissionID))
			next[0].ThreadID = firstNonEmpty(next[0].ThreadID, strings.TrimSpace(candidate.ThreadID))
			next[0].TurnID = firstNonEmpty(next[0].TurnID, strings.TrimSpace(candidate.TurnID))
			if next[0].StartedAt == 0 {
				next[0].StartedAt = candidate.StartedAt
			}
			continue
		}
		next = append(next, candidate)
	}
	sess.ActiveOperations = next
	sessionSyncLegacyActiveFields(sess)
}

func sessionRemoveActiveOperation(sess *state.Session, submissionID, turnID string) bool {
	if sess == nil {
		return false
	}
	sessionEnsureActiveOperations(sess)
	if len(sess.ActiveOperations) == 0 {
		return false
	}
	submissionID = strings.TrimSpace(submissionID)
	turnID = strings.TrimSpace(turnID)
	if submissionID == "" && turnID == "" {
		return false
	}

	next := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations))
	removed := false
	for _, op := range sess.ActiveOperations {
		if sessionActiveOperationMatches(op, submissionID, turnID) {
			removed = true
			continue
		}
		next = append(next, op)
	}
	if !removed {
		return false
	}
	sess.ActiveOperations = next
	sessionSyncLegacyActiveFields(sess)
	return true
}

func sessionFindActiveOperationByTurn(sess *state.Session, turnID string) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	sessionEnsureActiveOperations(sess)
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil
	}
	for i := len(sess.ActiveOperations) - 1; i >= 0; i-- {
		op := sess.ActiveOperations[i]
		if strings.TrimSpace(op.TurnID) == turnID {
			cp := op
			return &cp
		}
	}
	return nil
}

func sessionFindActiveOperationByThread(sess *state.Session, threadID string) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	sessionEnsureActiveOperations(sess)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	for i := len(sess.ActiveOperations) - 1; i >= 0; i-- {
		op := sess.ActiveOperations[i]
		if strings.TrimSpace(op.ThreadID) == threadID {
			cp := op
			return &cp
		}
	}
	return nil
}

func sessionFindPendingSubmissionOperationByThread(sess *state.Session, threadID string) *state.SessionActiveOperation {
	if sess == nil {
		return nil
	}
	sessionEnsureActiveOperations(sess)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	for i := 0; i < len(sess.ActiveOperations); i++ {
		op := sess.ActiveOperations[i]
		if strings.TrimSpace(op.Kind) != sessionOpKindSubmission {
			continue
		}
		if strings.TrimSpace(op.ThreadID) != threadID {
			continue
		}
		if strings.TrimSpace(op.TurnID) != "" {
			continue
		}
		cp := op
		return &cp
	}
	return nil
}

func sessionActiveOperationMatches(op state.SessionActiveOperation, submissionID, turnID string) bool {
	submissionID = strings.TrimSpace(submissionID)
	turnID = strings.TrimSpace(turnID)
	if submissionID != "" && strings.TrimSpace(op.SubmissionID) == submissionID {
		return true
	}
	if turnID != "" && strings.TrimSpace(op.TurnID) == turnID {
		return true
	}
	return false
}
