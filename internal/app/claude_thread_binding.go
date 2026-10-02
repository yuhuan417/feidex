package app

import (
	"feidex/internal/domain/conversation"
	"strings"

	"feidex/internal/app/apputil"
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

	appState := a.State()
	var workspaceID string

	if turnID != "" {
		if _, sub := newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn("", turnID); sub != nil {
			workspaceID = strings.TrimSpace(sub.WorkspaceID)
			_ = appState.UpdateSubmission(sub.ID, func(value *state.Submission) {
				value.ThreadID = threadID
				if strings.TrimSpace(value.TurnID) == "" {
					value.TurnID = turnID
				}
			})
			newRuntimeStateService(a).rebindTurnThreadID(turnID, threadID)
			if updated := appState.Submission(sub.ID); updated != nil {
				newReplyContinuationService(a).recordSubmissionSourceLinks(updated)
			}
		}
	}

	sess := appState.Session(sessionKey)
	if sess == nil {
		return
	}
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	targetOps := make([]conversation.SessionActiveOperation, 0, len(sess.ActiveOperations))
	conversation.EnsureActiveOperations(sess)
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
		_ = appState.UpdateSubmission(op.SubmissionID, func(value *state.Submission) {
			value.ThreadID = threadID
			if strings.TrimSpace(value.TurnID) == "" && strings.TrimSpace(op.TurnID) != "" {
				value.TurnID = strings.TrimSpace(op.TurnID)
			}
		})
		newRuntimeStateService(a).rebindTurnThreadID(op.TurnID, threadID)
		if updated := appState.Submission(op.SubmissionID); updated != nil {
			if workspaceID == "" {
				workspaceID = strings.TrimSpace(updated.WorkspaceID)
			}
			newReplyContinuationService(a).recordSubmissionSourceLinks(updated)
		}
	}
	updatedSess, _ := appState.UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		conversation.EnsureActiveOperations(current)
		for _, op := range targetOps {
			conversation.UpsertActiveOperation(current, conversation.SessionActiveOperation{
				Kind:         apputil.FirstNonEmpty(strings.TrimSpace(op.Kind), conversation.OpKindSubmission),
				SubmissionID: strings.TrimSpace(op.SubmissionID),
				ThreadID:     threadID,
				TurnID:       strings.TrimSpace(op.TurnID),
			})
		}
		conversation.SetThreadContext(current, workspaceID, threadID, current.ActiveThreadName, current.ActiveThreadPreview)
		if strings.TrimSpace(current.ActiveThreadName) == "" {
			current.ActiveThreadName = "Claude"
		}
	})
	markSessionThreadLive(a, sessionKey, threadID)

	if updatedSess != nil && strings.TrimSpace(updatedSess.RootMessageID) != "" && turnID != "" {
		newReplyContinuationService(a).recordRootTurnBinding(updatedSess.RootMessageID, sessionKey, threadID, turnID)
	}
}
