package app

import (
	"encoding/json"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

func handleNotification(a *App, method string, params json.RawMessage) {
	dispatchCodexNotification(a, method, params)
}

func onThreadTokenUsageUpdated(a *App, threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	newRuntimeStateService(a).recordTurnTokenUsage(threadID, turnID, usage)
}

func onTurnStartedNotification(a *App, threadID, turnID string) {
	newTurnLifecycleService(a).OnTurnStartedNotification(threadID, turnID)
}

func handleServerRequest(a *App, req codexrpc.RequestEnvelope) {
	dispatchCodexRequest(a, req)
}

func onCommandApproval(a *App, req codexrpc.RequestEnvelope) {
	req.Method = "item/commandExecution/requestApproval"
	dispatchCodexRequest(a, req)
}

func onFileApproval(a *App, req codexrpc.RequestEnvelope) {
	req.Method = "item/fileChange/requestApproval"
	dispatchCodexRequest(a, req)
}

func onPermissionsApproval(a *App, req codexrpc.RequestEnvelope) {
	req.Method = "item/permissions/requestApproval"
	dispatchCodexRequest(a, req)
}

func onToolUserInput(a *App, req codexrpc.RequestEnvelope) {
	req.Method = "item/tool/requestUserInput"
	dispatchCodexRequest(a, req)
}

func onMcpElicitationRequest(a *App, req codexrpc.RequestEnvelope) {
	req.Method = "mcpServer/elicitation/request"
	dispatchCodexRequest(a, req)
}

func finishTurn(a *App, threadID, turnID, status string) {
	// Steer-submission cleanup (which used to happen here) is now
	// handled inside turnLifecycleService.FinishTurn, synchronously
	// before the async StartNextSubmissionAsync is launched.  This
	// prevents a race where the newly started submission (same thread)
	// was incorrectly finalized by a post-hoc steer scan.
	newTurnLifecycleService(a).FinishTurn(threadID, turnID, status)
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
	case domainsubmission.SubmissionStatusCompleted.String():
		_ = st.FinalizeSubmission(submissionID, domainsubmission.SubmissionStatusCompleted.String())
	case domainsubmission.SubmissionStatusInterrupted.String():
		_ = st.FinalizeSubmission(submissionID, domainsubmission.SubmissionStatusInterrupted.String())
	default:
		_ = st.FinalizeSubmission(submissionID, domainsubmission.SubmissionStatusFailed.String())
	}
	newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
	// Remove the steer submission's ActiveOperation from the session.
	sessionKey := strings.TrimSpace(sub.SessionKey)
	if sessionKey != "" {
		turnID := strings.TrimSpace(sub.TurnID)
		st.UpdateSession(sessionKey, func(sess *conversation.Session) {
			if sess == nil {
				return
			}
			conversation.RemoveActiveOperation(sess, submissionID, turnID)
			if !conversation.HasActiveOperations(sess) {
				sess.Status = conversation.SessionStatusIdle.String()
			}
		})
	}
}

func startNextSubmissionAsync(a *App, sessionKey, source string) {
	newSubmissionQueueServiceFromApp(a).StartNextSubmissionAsync(sessionKey, source)
}
