package app

import (
	"encoding/json"
	"strings"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func handleNotification(a *App, method string, params json.RawMessage) {
	newCodexEventRouter(a).handleNotification(method, params)
}

func onThreadTokenUsageUpdated(a *App, threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	newRuntimeStateService(a).recordTurnTokenUsage(threadID, turnID, usage)
}

func onTurnStartedNotification(a *App, threadID, turnID string) {
	newTurnLifecycleService(a).onTurnStartedNotification(threadID, turnID)
}

func handleServerRequest(a *App, req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).handleServerRequest(req)
}

func onCommandApproval(a *App, req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onCommandApproval(req)
}

func onFileApproval(a *App, req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onFileApproval(req)
}

func onPermissionsApproval(a *App, req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onPermissionsApproval(req)
}

func onToolUserInput(a *App, req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onToolUserInput(req)
}

func onMcpElicitationRequest(a *App, req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onMcpElicitationRequest(req)
}

func finishTurn(a *App, threadID, turnID, status string) {
	newTurnLifecycleService(a).finishTurn(threadID, turnID, status)
	// Also finalize any steer submissions that were part of this turn.
	// The steer submission's ActiveOperation shares the thread but has its
	// own turnID. After the original turn is finished, scan for remaining
	// steer operations on the same thread and finalize them.
	finshSteerSubmissionsForThread(a, threadID, status)
}

// finishSteerSubmission finalizes a steer submission that was processed as
// part of the current conversation round. It finalizes the submission and
// removes its ActiveOperation from the session.
func finishSteerSubmission(a *App, submissionID, status string) {
	st := a.State()
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return
	}
	sub := st.Submission(submissionID)
	if sub == nil || sub.Finalized {
		return
	}
	switch status {
	case state.SubmissionStatusCompleted.String():
		_ = st.FinalizeSubmission(submissionID, state.SubmissionStatusCompleted.String())
	case state.SubmissionStatusInterrupted.String():
		_ = st.FinalizeSubmission(submissionID, state.SubmissionStatusInterrupted.String())
	default:
		_ = st.FinalizeSubmission(submissionID, state.SubmissionStatusFailed.String())
	}
	newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
	// Remove the steer submission's ActiveOperation from the session.
	sessionKey := strings.TrimSpace(sub.SessionKey)
	if sessionKey != "" {
		turnID := strings.TrimSpace(sub.TurnID)
		st.UpdateSession(sessionKey, func(sess *state.Session) {
			if sess == nil {
				return
			}
			sessionRemoveActiveOperation(sess, submissionID, turnID)
			if !sessionHasActiveOperations(sess) {
				sess.Status = state.SessionStatusIdle.String()
			}
		})
	}
}

// finshSteerSubmissionsForThread scans the session's ActiveOperations after
// a turn completes and finalizes any remaining submissions (which are steer
// submissions that were part of the same conversation round).
func finshSteerSubmissionsForThread(a *App, threadID, status string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	st := a.State()
	for _, sess := range st.Sessions() {
		if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		for _, op := range sess.ActiveOperations {
			subID := strings.TrimSpace(op.SubmissionID)
			if subID == "" {
				continue
			}
			sub := st.Submission(subID)
			if sub == nil || sub.Finalized {
				continue
			}
			switch status {
			case state.SubmissionStatusCompleted.String():
				_ = st.FinalizeSubmission(subID, state.SubmissionStatusCompleted.String())
			case state.SubmissionStatusInterrupted.String():
				_ = st.FinalizeSubmission(subID, state.SubmissionStatusInterrupted.String())
			default:
				_ = st.FinalizeSubmission(subID, state.SubmissionStatusFailed.String())
			}
			newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
			st.UpdateSession(sess.Key, func(s *state.Session) {
				if s == nil {
					return
				}
				sessionRemoveActiveOperation(s, subID, strings.TrimSpace(op.TurnID))
				if !sessionHasActiveOperations(s) {
					s.Status = state.SessionStatusIdle.String()
				}
			})
		}
	}
}

func startNextSubmissionAsync(a *App, sessionKey, source string) {
	newSubmissionCoordinator(a).startNextSubmissionAsync(sessionKey, source)
}
