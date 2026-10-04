package feishuapp

import (
	"encoding/json"
	"feidex/internal/application/submission"
	"feidex/internal/application/turn"
	"feidex/internal/codexrpc"
)

func handleNotification(a *App, method string, params json.RawMessage) {
	dispatchCodexNotification(a, method, params)
}

func onTurnStartedNotification(turns *turn.Service, threadID, turnID string) {
	turns.OnTurnStartedNotification(threadID, turnID)
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

func finishTurn(turns *turn.Service, threadID, turnID, status string) {
	// Steer-submission cleanup (which used to happen here) is now
	// handled inside turnLifecycleService.FinishTurn, synchronously
	// before the async StartNextSubmissionAsync is launched.  This
	// prevents a race where the newly started submission (same thread)
	// was incorrectly finalized by a post-hoc steer scan.
	turns.FinishTurn(threadID, turnID, status)
}

func startNextSubmissionAsync(submissions *submission.SubmissionQueueService, sessionKey, source string) {
	submissions.StartNextSubmissionAsync(sessionKey, source)
}
