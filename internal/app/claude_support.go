package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/apputil"
	appclauderuntime "feidex/internal/app/clauderuntime"
	"feidex/internal/app/claudesupport"
	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newClaudeSupportService(a *App) *claudesupport.Service {
	return &claudesupport.Service{
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
		DeliverDetachedPendingCard: func(card map[string]any, target appclauderuntime.InteractionTarget, reqKey, reqIDStored, backend, kind, payloadJSON, linkKind string) error {
			return deliverDetachedPendingCard(a, detachedCardAnchor(target), card, pendingCardDelivery{
				requestKey:      reqKey,
				requestIDStored: reqIDStored,
				backend:         backend,
				kind:            kind,
				sessionKey:      strings.TrimSpace(target.SessionKey),
				threadID:        strings.TrimSpace(target.ThreadID),
				turnID:          strings.TrimSpace(target.TurnID),
				itemID:          reqKey,
				ownerUserID:     strings.TrimSpace(target.UserID),
				payloadJSON:     payloadJSON,
				linkKind:        linkKind,
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
		PrepareMentionText: apputil.PrependAttentionMentionMarkdown,
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

func claudePlanCancelledBody(pending *state.PendingRequest) string {
	return claudesupport.ClaudePlanCancelledBody(pending)
}

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
