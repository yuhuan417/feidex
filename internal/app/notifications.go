package app

import (
	"encoding/json"

	"feidex/internal/codexrpc"
)

func handleNotification(a *App, method string, params json.RawMessage) {
	newCodexEventRouter(a).handleNotification(method, params)
}

func onThreadTokenUsageUpdated(a *App, threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	newRuntimeStateService(a).recordTurnTokenUsage(threadID, turnID, usage)
}

func onTurnStartedNotification(a *App, threadID, turnID string) {
	newLifecycleCoordinator(a).onTurnStartedNotification(threadID, turnID)
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
	newLifecycleCoordinator(a).finishTurn(threadID, turnID, status)
}

func startNextSubmissionAsync(a *App, sessionKey, source string) {
	newLifecycleCoordinator(a).startNextSubmissionAsync(sessionKey, source)
}
