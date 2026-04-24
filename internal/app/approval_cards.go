package app

import (
	"encoding/json"
	appapproval "feidex/internal/app/approval"
	"strings"

	"feidex/internal/feishu"
)

func (s outboundCardService) sendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	newOutboundCardService(s.app).sendApprovalCardWithPayload(kind, requestID, threadID, turnID, itemID, body, nil)
}

func (s outboundCardService) sendApprovalCardWithPayload(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string, requestPayload map[string]any) {
	sessionKey, sub := findSubmissionByTurn(s.app, threadID, turnID)
	if sub == nil {
		replyCodexError(s.app, requestID, -32602, "no active session for approval")
		return
	}
	requestKey := requestIDKey(requestID)
	buttons := appapproval.Buttons(kind, requestKey, requestPayload)
	card := renderApprovalCard(s.app, sessionKey, sub, "等待审批", "orange", strings.TrimSpace(body), buttons)
	payload := map[string]any{}
	if strings.TrimSpace(body) != "" {
		payload["body"] = body
	}
	if len(requestPayload) > 0 {
		payload["request"] = requestPayload
	}
	err := deliverPendingCard(s.app, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            kind,
		sessionKey:      sessionKey,
		threadID:        threadID,
		turnID:          turnID,
		itemID:          itemID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_approval",
		linkKind:        "approval_card",
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
	sessionKey, sub := findSubmissionByTurn(s.app, threadID, turnID)
	if sub == nil {
		replyCodexError(s.app, requestID, -32602, "no active session for permissions approval")
		return
	}
	requestKey := requestIDKey(requestID)
	card := renderApprovalCard(s.app, sessionKey, sub, "权限请求", "orange", strings.TrimSpace(body), []feishu.Button{
		{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestKey}},
		{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestKey}},
	})
	payload := map[string]any{"permissions": permissions}
	if strings.TrimSpace(body) != "" {
		payload["body"] = body
	}
	if len(requestPayload) > 0 {
		payload["request"] = requestPayload
	}
	err := deliverPendingCard(s.app, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "permissions",
		sessionKey:      sessionKey,
		threadID:        threadID,
		turnID:          turnID,
		itemID:          itemID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_approval",
		linkKind:        "permissions_card",
	})
	if err == nil {
		return
	}
	replyCodexError(s.app, requestID, -32603, err.Error())
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
		waitingStatus:   "waiting_user_input",
		linkKind:        "user_input_card",
	})
	if err == nil {
		return
	}
	replyCodexError(s.app, requestID, -32603, err.Error())
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
