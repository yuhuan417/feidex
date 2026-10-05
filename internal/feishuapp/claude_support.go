package feishuapp

import (
	"context"
	domainbackend "feidex/internal/domain/backend"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"strings"
	"time"

	appapproval "feidex/internal/adapter/feishu/approval"
	appstate "feidex/internal/adapter/storage/json/scoped"
	apputil "feidex/internal/formatutil"

	"feidex/internal/adapter/feishu/claudesupport"
	"feidex/internal/adapter/feishu/pendingforms"
	"feidex/internal/adapter/feishu/serverrequest"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	appclauderuntime "feidex/internal/runtime/claude"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ClaudeSupportInputs struct {
	State               *appstate.Store
	Feishu              FeishuClient
	PendingReplies      PendingReplyAdapter
	CancelPending       func(*state.PendingRequest) error
	PendingCards        PendingCardDeliveryService
	EffectRunner        frontendruntime.EffectRunner
	FrontendID          identity.FrontendID
	ClaudeCore          func() ClaudeCore
	WorkspaceConfigured bool
}

func BuildClaudeSupport(inputs ClaudeSupportInputs) *claudesupport.Service {
	effectRunner := inputs.EffectRunner
	frontendID := inputs.FrontendID

	return &claudesupport.Service{
		DeliverPendingCard: func(sub *domainsubmission.Submission, card map[string]any, reqKey, reqIDStored, backend, kind, sessionKey, threadID, turnID, itemID, ownerUserID, payloadJSON, waitingStatus, linkKind string, ttl time.Duration) error {
			if sub == nil {
				return fmt.Errorf("pending card delivery unavailable")
			}
			return inputs.PendingCards.Deliver(anchorForSubmission(sub), card, pendingCardDelivery{
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
			delivery := pendingCardDelivery{
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
			}
			delivery.nonBlocking = true
			return inputs.PendingCards.Deliver(detachedCardAnchor(target), card, delivery)
		},
		RenderApprovalCard: func(sub *domainsubmission.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
			return renderApprovalCard(inputs.State, inputs.Feishu, sub, title, color, body, buttons)
		},
		SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
			if inputs.Feishu == nil {
				return nil
			}
			return inputs.Feishu.SimpleStatusCard(title, color, body, buttons)
		},
		PatchCard: func(messageID string, card map[string]any) error {
			return patchCardEffect(context.Background(), effectRunner, string(frontendID), messageID, card)
		},
		PrepareMentionText: apputil.PrependAttentionMentionMarkdown,
		RenderFormCard:     pendingforms.RenderToolUserInputFormCard,
		ContentCardTitle: func(sessionKey, workspaceID, title string) string {
			return contentCardTitleForSession(inputs.State, inputs.WorkspaceConfigured, sessionKey, workspaceID, title)
		},
		BackendClaude: domainbackend.BackendClaude,
		ResolvePlanFeedback: func(pendingID, feedback string) error {
			if inputs.ClaudeCore == nil {
				return fmt.Errorf("Claude runtime unavailable")
			}
			core := inputs.ClaudeCore()
			if core == nil {
				return fmt.Errorf("Claude runtime unavailable")
			}
			return core.ResolvePlanFeedback(pendingID, feedback)
		},
		FinalizePendingReply: func(pending *state.PendingRequest) *state.PendingRequest {
			return inputs.PendingReplies.Finalize(pending)
		},
		CancelPending: func(pending *state.PendingRequest) error {
			if inputs.CancelPending == nil {
				return fmt.Errorf("pending request cancellation unavailable")
			}
			return inputs.CancelPending(pending)
		},
		RawCard: rawCard,
		PendingLookup: func(requestID string) *state.PendingRequest {
			if inputs.State == nil {
				return nil
			}
			return inputs.State.Pending(requestID)
		},
	}
}

func claudePlanCancelledBody(pending *state.PendingRequest) string {
	return claudesupport.ClaudePlanCancelledBody(pending)
}

func sendClaudeApprovalCardWithPayload(claudesupportDep *claudesupport.Service, kind, requestID, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error {
	return claudesupportDep.SendApprovalCardWithPayload(sub, kind, requestID, sessionKey, threadID, turnID, itemID, body, requestPayload, sessionActionLabel)
}

func sendClaudeApprovalCard(claudesupportDep *claudesupport.Service, requestID, sessionKey string, sub *domainsubmission.Submission, presentation appapproval.Presentation) error {
	return sendClaudeApprovalCardWithPayload(
		claudesupportDep,
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

func sendClaudeUserInputCard(claudesupportDep *claudesupport.Service, requestID, sessionKey string, sub *domainsubmission.Submission, payload pendingforms.ToolUserInputPayload) error {
	return claudesupportDep.SendUserInputCard(sub, requestID, sessionKey, payload)
}

func sendClaudeUserInputFormCard(claudesupportDep *claudesupport.Service, requestID, sessionKey string, sub *domainsubmission.Submission, payload pendingforms.ToolUserInputPayload) error {
	return claudesupportDep.SendUserInputFormCard(sub, requestID, sessionKey, payload)
}

func sendClaudePlanModeCard(claudesupportDep *claudesupport.Service, requestID, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, body string) error {
	return claudesupportDep.SendPlanModeCard(sub, requestID, sessionKey, threadID, turnID, body)
}

func completeClaudePlanModeText(claudesupportDep *claudesupport.Service, msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	feedback := strings.TrimSpace(msg.Text)
	if feedback == "" {
		return fmt.Errorf("反馈不能为空")
	}
	return claudesupportDep.CompletePlanModeText(feedback, pending)
}

func completePlanApprove(claudesupportDep *claudesupport.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	result, err := claudesupportDep.CompletePlanApprove(requestID, action.UserID)
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

func completePlanReject(claude *claudesupport.Service, requests *serverrequest.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	result, err := claude.CompletePlanReject(requestID, action.UserID, func(pending *state.PendingRequest) error {
		return requests.AdapterForPending(pending).CancelPending(pending)
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
