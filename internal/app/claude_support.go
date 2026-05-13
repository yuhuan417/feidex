package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/claudesupport"
	"feidex/internal/app/pendingforms"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Service type and constructor
// ---------------------------------------------------------------------------

type claudeSupportService = claudesupport.Service

func newClaudeSupportService(a *App) *claudeSupportService {
	return &claudeSupportService{
		DeliverPendingCard: func(sub *state.Submission, card map[string]any, reqKey, reqIDStored, backend, kind, sessionKey, threadID, turnID, itemID, ownerUserID, payloadJSON, waitingStatus, linkKind string, ttl time.Duration) error {
			return deliverPendingCard(a, sub, card, pendingCardDelivery{
				requestKey:      reqKey,
				requestIDStored: reqIDStored,
				backend:         backend,
				kind:            kind,
				sessionKey:      sessionKey,
				threadID:        threadID,
				turnID:          turnID,
				itemID:          itemID,
				ownerUserID:     ownerUserID,
				payloadJSON:     payloadJSON,
				waitingStatus:   waitingStatus,
				linkKind:        linkKind,
				ttl:             ttl,
			})
		},
		RenderApprovalCard: func(sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
			return renderApprovalCard(a, "", sub, title, color, body, buttons)
		},
		SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
			return a.feishu.SimpleStatusCard(title, color, body, buttons)
		},
		PatchCard: func(messageID string, card map[string]any) error {
			return a.feishu.PatchCard(context.Background(), messageID, card)
		},
		PrepareMentionText: prependAttentionMentionMarkdown,
		RenderFormCard:     pendingforms.RenderToolUserInputFormCard,
		ContentCardTitle: func(sessionKey, workspaceID, title string) string {
			return contentCardTitleForSession(a, sessionKey, workspaceID, title)
		},
		BackendClaude: backendClaude,
		ResolvePlanFeedback: func(pendingID, feedback string) error {
			return a.claude.ResolvePlanFeedback(pendingID, feedback)
		},
		FinalizePendingReply: func(pending *state.PendingRequest) *state.PendingRequest {
			return newRuntimeStateService(a).finalizePendingReply(pending)
		},
		CancelPending: func(pending *state.PendingRequest) error {
			return a.ServerRequestService().AdapterForPending(pending).CancelPending(pending)
		},
		RawCard: rawCard,
		PendingLookup: func(requestID string) *state.PendingRequest {
			return a.State().Pending(requestID)
		},
	}
}

// ---------------------------------------------------------------------------
// Thin wrappers — pure helper functions (already delegated)
// ---------------------------------------------------------------------------

func claudeRequestIDStored(requestID string) string {
	return claudesupport.ClaudeRequestIDStored(requestID)
}

func claudeApprovalButtons(kind, requestKey, sessionActionLabel string) []feishu.Button {
	return claudesupport.ClaudeApprovalButtons(kind, requestKey, sessionActionLabel)
}

func normalizeClaudeSessionPermissionUpdate(update map[string]any) (map[string]any, bool) {
	return claudesupport.NormalizeClaudeSessionPermissionUpdate(update)
}

func claudeApprovalResolutionForAction(actionName string) (appruntime.ClaudeApprovalResolution, string) {
	return claudesupport.ClaudeApprovalResolutionForAction(actionName)
}

func claudeAnswersFromSelections(payload pendingforms.ToolUserInputPayload, selections map[string]string) (map[string]string, string, error) {
	return claudesupport.ClaudeAnswersFromSelections(payload, selections)
}

func parseClaudeToolUserInputResponse(text string, payload pendingforms.ToolUserInputPayload) (map[string]string, string, error) {
	return claudesupport.ParseClaudeToolUserInputResponse(text, payload)
}

func claudeQuestionAnswer(raw string, q pendingforms.ToolUserInputQuestion) (string, string, error) {
	return claudesupport.ClaudeQuestionAnswer(raw, q)
}

func claudePlanSubmittedBody(pending *state.PendingRequest, feedback string) string {
	return claudesupport.ClaudePlanSubmittedBody(pending, feedback)
}

func claudePlanCancelledBody(pending *state.PendingRequest) string {
	return claudesupport.ClaudePlanCancelledBody(pending)
}

func claudePlanOriginalBody(pending *state.PendingRequest) string {
	return claudesupport.ClaudePlanOriginalBody(pending)
}

// ---------------------------------------------------------------------------
// Thin wrappers — Service method delegates
// ---------------------------------------------------------------------------

func sendClaudeApprovalCardWithPayload(a *App, kind, requestID, sessionKey string, sub *state.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error {
	return newClaudeSupportService(a).SendApprovalCardWithPayload(sub, kind, requestID, sessionKey, threadID, turnID, itemID, body, requestPayload, sessionActionLabel)
}

func sendClaudeApprovalCard(a *App, requestID, sessionKey string, sub *state.Submission, presentation appapproval.Presentation) error {
	return sendClaudeApprovalCardWithPayload(
		a,
		presentation.Kind.String(),
		requestID,
		sessionKey,
		sub,
		presentation.ThreadID,
		presentation.TurnID,
		presentation.ItemID,
		presentation.Body,
		presentation.Payload.Request,
		presentation.Payload.SessionActionLabel,
	)
}

func sendClaudeUserInputCard(a *App, requestID, sessionKey string, sub *state.Submission, payload pendingforms.ToolUserInputPayload) error {
	return newClaudeSupportService(a).SendUserInputCard(sub, requestID, sessionKey, payload)
}

func sendClaudeUserInputFormCard(a *App, requestID, sessionKey string, sub *state.Submission, payload pendingforms.ToolUserInputPayload) error {
	return newClaudeSupportService(a).SendUserInputFormCard(sub, requestID, sessionKey, payload)
}

func sendClaudePlanModeCard(a *App, requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error {
	return newClaudeSupportService(a).SendPlanModeCard(sub, requestID, sessionKey, threadID, turnID, body)
}

func (s pendingInputService) completeClaudePlanModeText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	feedback := strings.TrimSpace(msg.Text)
	if feedback == "" {
		return fmt.Errorf("反馈不能为空")
	}
	return newClaudeSupportService(s.app).CompletePlanModeText(feedback, pending)
}

func completePlanApprove(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	result, err := newClaudeSupportService(a).CompletePlanApprove(requestID, action.UserID)
	if err != nil {
		return nil, err
	}
	resp := &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: result.ToastType, Content: result.ToastContent},
	}
	if result.CardMap != nil {
		resp.Card = rawCard(result.CardMap)
	}
	return resp, nil
}

func completePlanReject(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	svc := newClaudeSupportService(a)
	result, err := svc.CompletePlanReject(requestID, action.UserID, func(pending *state.PendingRequest) error {
		return a.ServerRequestService().AdapterForPending(pending).CancelPending(pending)
	})
	if err != nil {
		return nil, err
	}
	resp := &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: result.ToastType, Content: result.ToastContent},
	}
	if result.CardMap != nil {
		resp.Card = rawCard(result.CardMap)
	}
	return resp, nil
}
