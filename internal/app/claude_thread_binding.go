package app

import (
	"strings"

	"feidex/internal/state"
)

func bindClaudeSessionThread(a *App, sessionKey, turnID, threadID string) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	turnID = strings.TrimSpace(turnID)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return
	}

	appState := appState(a)
	var workspaceID string

	if turnID != "" {
		if _, sub := findSubmissionByTurn(a, "", turnID); sub != nil {
			workspaceID = strings.TrimSpace(sub.WorkspaceID)
			_ = appState.updateSubmission(sub.ID, func(value *state.Submission) {
				value.ThreadID = threadID
				if strings.TrimSpace(value.TurnID) == "" {
					value.TurnID = turnID
				}
			})
			newRuntimeStateService(a).rebindTurnThreadID(turnID, threadID)
			if updated := appState.submission(sub.ID); updated != nil {
				newReplyContinuationService(a).recordSubmissionSourceLinks(updated)
			}
		}
	}

	sess := appState.session(sessionKey)
	if sess == nil {
		return
	}
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	targetOps := make([]state.SessionActiveOperation, 0, len(sess.ActiveOperations))
	sessionEnsureActiveOperations(sess)
	for _, op := range sess.ActiveOperations {
		if strings.TrimSpace(op.TurnID) == "" {
			continue
		}
		if turnID != "" && strings.TrimSpace(op.TurnID) != turnID {
			continue
		}
		targetOps = append(targetOps, op)
	}
	for _, op := range targetOps {
		if strings.TrimSpace(op.SubmissionID) == "" {
			continue
		}
		_ = appState.updateSubmission(op.SubmissionID, func(value *state.Submission) {
			value.ThreadID = threadID
			if strings.TrimSpace(value.TurnID) == "" && strings.TrimSpace(op.TurnID) != "" {
				value.TurnID = strings.TrimSpace(op.TurnID)
			}
		})
		newRuntimeStateService(a).rebindTurnThreadID(op.TurnID, threadID)
		if updated := appState.submission(op.SubmissionID); updated != nil {
			if workspaceID == "" {
				workspaceID = strings.TrimSpace(updated.WorkspaceID)
			}
			newReplyContinuationService(a).recordSubmissionSourceLinks(updated)
		}
	}
	updatedSess, _ := appState.updateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		sessionEnsureActiveOperations(current)
		for _, op := range targetOps {
			sessionUpsertActiveOperation(current, state.SessionActiveOperation{
				Kind:         firstNonEmpty(strings.TrimSpace(op.Kind), sessionOpKindSubmission),
				SubmissionID: strings.TrimSpace(op.SubmissionID),
				ThreadID:     threadID,
				TurnID:       strings.TrimSpace(op.TurnID),
			})
		}
		setSessionThreadContext(current, workspaceID, threadID, current.ActiveThreadName, current.ActiveThreadPreview)
		if strings.TrimSpace(current.ActiveThreadName) == "" {
			current.ActiveThreadName = "Claude"
		}
	})
	markSessionThreadLive(a, sessionKey, threadID)

	if updatedSess != nil && strings.TrimSpace(updatedSess.RootMessageID) != "" && turnID != "" {
		newReplyContinuationService(a).recordRootTurnBinding(updatedSess.RootMessageID, sessionKey, threadID, turnID)
	}
}
