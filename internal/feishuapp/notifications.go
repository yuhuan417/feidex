package feishuapp

import (
	"encoding/json"
	"feidex/internal/codexrpc"
)

func handleNotification(a *App, method string, params json.RawMessage) {
	dispatchCodexNotification(a, method, params)
}

func onTurnStartedNotification(a *App, threadID, turnID string) {
	a.bindings.Turns.OnTurnStartedNotification(threadID, turnID)
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
	a.bindings.Turns.FinishTurn(threadID, turnID, status)
}

func startNextSubmissionAsync(a *App, sessionKey, source string) {
	a.bindings.Submissions.StartNextSubmissionAsync(sessionKey, source)
}
