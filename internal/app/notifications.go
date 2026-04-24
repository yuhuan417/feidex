package app

import (
	"encoding/json"

	"feidex/internal/codexrpc"
)

func (a *App) handleNotification(method string, params json.RawMessage) {
	newCodexEventRouter(a).handleNotification(method, params)
}

func (a *App) onThreadTokenUsageUpdated(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	newRuntimeStateService(a).recordTurnTokenUsage(threadID, turnID, usage)
}

func (a *App) onTurnStartedNotification(threadID, turnID string) {
	newLifecycleCoordinator(a).onTurnStartedNotification(threadID, turnID)
}

func (a *App) handleServerRequest(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).handleServerRequest(req)
}

func (a *App) onCommandApproval(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onCommandApproval(req)
}

func (a *App) onFileApproval(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onFileApproval(req)
}

func (a *App) onPermissionsApproval(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onPermissionsApproval(req)
}

func (a *App) onToolUserInput(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onToolUserInput(req)
}

func (a *App) onMcpElicitationRequest(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onMcpElicitationRequest(req)
}

func (a *App) finishTurn(threadID, turnID, status string) {
	newLifecycleCoordinator(a).finishTurn(threadID, turnID, status)
}

func (a *App) startNextSubmissionAsync(sessionKey, source string) {
	newLifecycleCoordinator(a).startNextSubmissionAsync(sessionKey, source)
}
