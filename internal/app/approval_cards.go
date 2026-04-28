package app

import (
	"encoding/json"
	appapproval "feidex/internal/app/approval"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

type outboundCardService struct {
	app *App
}
func newOutboundCardService(app *App) outboundCardService {
	return outboundCardService{app: app}
}

func (s outboundCardService) sendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	newOutboundCardService(s.app).sendApprovalCardWithPayload(kind, requestID, threadID, turnID, itemID, body, nil)
}

func (s outboundCardService) sendApprovalCardWithPayload(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string, requestPayload map[string]any) {
	newOutboundCardService(s.app).sendApprovalCardPresentation(requestID, appapproval.Presentation{
		Kind:     appapproval.NormalizeKind(kind),
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Body:     body,
		Payload: appapproval.RequestPayload{
			Body:    strings.TrimSpace(body),
			Request: requestPayload,
		},
	})
}

func (s outboundCardService) sendApprovalCardPresentation(requestID json.RawMessage, presentation appapproval.Presentation) {
	sessionKey, sub := findSubmissionByTurn(s.app, presentation.ThreadID, presentation.TurnID)
	if sub == nil {
		replyCodexError(s.app, requestID, -32602, "no active session for approval")
		return
	}
	requestKey := requestIDKey(requestID)
	kind := presentation.Kind
	if kind == "" {
		kind = appapproval.NormalizeKind(sub.Kind)
	}
	title := "等待审批"
	linkKind := "approval_card"
	if kind == appapproval.KindPermissions {
		title = "权限请求"
		linkKind = "permissions_card"
	}
	buttons := appapproval.Buttons(kind.String(), requestKey, presentation.Payload.Request)
	card := renderApprovalCard(s.app, sessionKey, sub, title, "orange", strings.TrimSpace(presentation.Body), buttons)
	err := deliverPendingCard(s.app, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            kind.String(),
		sessionKey:      sessionKey,
		threadID:        presentation.ThreadID,
		turnID:          presentation.TurnID,
		itemID:          presentation.ItemID,
		ownerUserID:     sub.UserID,
		payloadJSON:     presentation.Payload.MarshalJSONText(),
		waitingStatus:   state.SubmissionStatusWaitingApproval.String(),
		linkKind:        linkKind,
	})
	if err == nil {
		return
	}
	replyCodexError(s.app, requestID, -32603, err.Error())
}

func (s outboundCardService) sendPermissionsCard(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any) {
	newOutboundCardService(s.app).sendPermissionsCardWithPayload(requestID, threadID, turnID, itemID, body, permissions, nil)
}

func (s outboundCardService) sendPermissionsCardWithPayload(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any, requestPayload map[string]any) {
	newOutboundCardService(s.app).sendApprovalCardPresentation(requestID, appapproval.Presentation{
		Kind:     appapproval.KindPermissions,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		Body:     body,
		Payload: appapproval.RequestPayload{
			Body:        strings.TrimSpace(body),
			Request:     requestPayload,
			Permissions: permissions,
		},
	})
}

func (s outboundCardService) sendUserInputCard(requestID json.RawMessage, payload toolUserInputPayload) {
	sessionKey, sub := findSubmissionByTurn(s.app, payload.ThreadID, payload.TurnID)
	if sub == nil || len(payload.Questions) == 0 {
		replyCodexError(s.app, requestID, -32602, "no active session for request_user_input")
		return
	}
	q := payload.Questions[0]
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		buttons = append(buttons, feishu.Button{
			Text: opt.Label,
			Type: "default",
			Value: map[string]any{
				"action":      "user_input.answer",
				"request_id":  requestIDKey(requestID),
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := s.app.feishu.SimpleStatusCard("需要补充输入", "orange", prependAttentionMentionMarkdown(q.Question, sub.UserID), buttons)
	requestKey := requestIDKey(requestID)
	err := deliverPendingCard(s.app, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "tool_request_user_input",
		sessionKey:      sessionKey,
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		itemID:          payload.ItemID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		linkKind:        "user_input_card",
	})
	if err == nil {
		return
	}
	replyCodexError(s.app, requestID, -32603, err.Error())
}
