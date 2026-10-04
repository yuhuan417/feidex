package feishuapp

import (
	"encoding/json"
	"feidex/internal/application/submission"
	"feidex/internal/application/turn"
	"feidex/internal/codexrpc"
)

func handleNotification(d BackendRuntimeDeps, method string, params json.RawMessage) {
	dispatchCodexNotification(d, method, params)
}

func onTurnStartedNotification(turns *turn.Service, threadID, turnID string) {
	turns.OnTurnStartedNotification(threadID, turnID)
}

func handleServerRequest(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	dispatchCodexRequest(d, req)
}

func onCommandApproval(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	req.Method = "item/commandExecution/requestApproval"
	dispatchCodexRequest(d, req)
}

func onFileApproval(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	req.Method = "item/fileChange/requestApproval"
	dispatchCodexRequest(d, req)
}

func onPermissionsApproval(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	req.Method = "item/permissions/requestApproval"
	dispatchCodexRequest(d, req)
}

func onToolUserInput(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	req.Method = "item/tool/requestUserInput"
	dispatchCodexRequest(d, req)
}

func onMcpElicitationRequest(d BackendRuntimeDeps, req codexrpc.RequestEnvelope) {
	req.Method = "mcpServer/elicitation/request"
	dispatchCodexRequest(d, req)
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
