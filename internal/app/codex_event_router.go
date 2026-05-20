package app

import (
	"context"
	"encoding/json"

	appapproval "feidex/internal/app/approval"
	appbackend "feidex/internal/app/backend"
	apppendingforms "feidex/internal/app/pendingforms"
	"feidex/internal/app/turnitem"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

// codexEventRouter wraps backend.CodexEventRouter with *App callbacks.
type codexEventRouter struct {
	app   *App
	inner *appbackend.CodexEventRouter
}

func newCodexEventRouter(app *App) *codexEventRouter {
	r := &codexEventRouter{app: app}
	r.inner = r.buildInner()
	return r
}

func (r *codexEventRouter) buildInner() *appbackend.CodexEventRouter {
	a := r.app
	router := appbackend.NewCodexEventRouter()
	router.NoteTurnItemStarted = func(threadID, turnID string, item turnitem.ProtocolItem) {
		newRuntimeStateService(a).noteTurnItemStartedPayload(threadID, turnID, item)
		noteStandaloneCompactItemStarted(a, threadID, turnID, item.MergedRaw())
		if normalizeTurnItemType(item.Type) == "mcp_tool_call" {
			newTurnStreamService(a).updateInFlightTurnItemPayload(context.Background(), threadID, turnID, item.EffectiveID(""), item)
		}
	}
	router.CompleteTurnItem = func(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
		newTurnStreamService(a).completeTurnItemPayload(ctx, threadID, turnID, itemID, item)
	}
	router.UpdateInFlightTurnItem = func(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
		snapshot := newRuntimeStateService(a).updateInFlightTurnItemPayload(threadID, turnID, itemID, item.MergedRaw())
		if quietWorkingCardEnabled(feishuConfig(a)) {
			newTurnStreamService(a).updateInFlightTurnItemPayload(ctx, threadID, turnID, itemID, snapshot)
		}
	}
	router.UpdatePendingPlan = func(turnID, plan string) {
		newTurnStreamService(a).updatePendingPlan(turnID, plan)
	}
	router.OnTurnStarted = func(threadID, turnID string) {
		onTurnStartedNotification(a, threadID, turnID)
	}
	router.OnTurnCompleted = func(threadID, turnID, status string) {
		finishTurn(a, threadID, turnID, status)
	}
	router.OnThreadTokenUsageUpdated = func(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
		onThreadTokenUsageUpdated(a, threadID, turnID, usage)
	}
	router.FailStandaloneCompactTurn = func(threadID, turnID, message string) bool {
		return failStandaloneCompactTurn(a, threadID, turnID, message)
	}
	router.RecordTurnError = func(threadID, turnID, message string) {
		newTurnStreamService(a).recordTurnError(threadID, turnID, message)
	}
	router.UpdateSubmissionByTurn = func(threadID, turnID string, mutate func(*state.Submission)) {
		newSubmissionQueueServiceFromApp(a).UpdateSubmissionByTurn(threadID, turnID, mutate)
	}
	router.ResolveServerPendingRequest = func(requestID string) *state.PendingRequest {
		return newRuntimeStateService(a).resolveServerPendingRequest(requestID)
	}
	router.ResumeSubmissionAfterRequest = func(pending *state.PendingRequest) {
		a.ServerRequestService().ResumeSubmissionAfterRequest(pending)
	}
	router.MergeApprovalPresentationWithTurnItem = func(presentation appapproval.Presentation) appapproval.Presentation {
		return newRuntimeStateService(a).mergeApprovalPresentationWithTurnItem(presentation)
	}
	router.SendApprovalCard = func(requestID json.RawMessage, presentation appapproval.Presentation) {
		a.ServerRequestService().SendApprovalCardPresentation(requestID, presentation)
	}
	router.SendUserInputCard = func(requestID json.RawMessage, payload apppendingforms.ToolUserInputPayload) {
		a.ServerRequestService().SendUserInputCard(requestID, payload)
	}
	router.SendUserInputFormCard = func(requestID json.RawMessage, payload apppendingforms.ToolUserInputPayload) {
		a.ServerRequestService().SendUserInputFormCard(requestID, payload)
	}
	router.SendElicitationURLCard = func(requestID json.RawMessage, payload apppendingforms.ElicitationURLPayload) {
		a.ServerRequestService().SendElicitationURLCard(requestID, payload)
	}
	router.SendElicitationFormCard = func(requestID json.RawMessage, payload apppendingforms.ElicitationFormPayload) {
		a.ServerRequestService().SendElicitationFormCard(requestID, payload)
	}
	router.ReplyCodexError = func(requestID json.RawMessage, code int, message string) {
		replyCodexError(a, requestID, code, message)
	}
	router.FindSubmissionByTurn = func(threadID, turnID string) (string, *state.Submission) {
		return newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn(threadID, turnID)
	}
	router.FindWorkspaceCwdForSubmission = func(sub *state.Submission) string {
		if sub == nil {
			return ""
		}
		if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
			return ws.Cwd
		}
		return ""
	}
	return router
}

func (r *codexEventRouter) handleNotification(method string, params json.RawMessage) {
	r.inner.HandleNotification(method, params)
}

func (r *codexEventRouter) handleServerRequest(req codexrpc.RequestEnvelope) {
	r.inner.HandleServerRequest(req)
}

// Server request handler methods — these delegate to the backend router
// and are called from the standalone functions in notifications.go.

func (r *codexEventRouter) onCommandApproval(req codexrpc.RequestEnvelope) {
	r.inner.OnCommandApproval(req)
}

func (r *codexEventRouter) onFileApproval(req codexrpc.RequestEnvelope) {
	r.inner.OnFileApproval(req)
}

func (r *codexEventRouter) onPermissionsApproval(req codexrpc.RequestEnvelope) {
	r.inner.OnPermissionsApproval(req)
}

func (r *codexEventRouter) onToolUserInput(req codexrpc.RequestEnvelope) {
	r.inner.OnToolUserInput(req)
}

func (r *codexEventRouter) onMcpElicitationRequest(req codexrpc.RequestEnvelope) {
	r.inner.OnMcpElicitationRequest(req)
}
