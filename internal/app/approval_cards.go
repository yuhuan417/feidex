package app

import (
	"encoding/json"
	"strings"

	"feidex/internal/feishu"
)

func (a *App) sendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	a.sendApprovalCardWithPayload(kind, requestID, threadID, turnID, itemID, body, nil)
}

func (a *App) sendApprovalCardWithPayload(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string, requestPayload map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		a.replyCodexError(requestID, -32602, "no active session for approval")
		return
	}
	requestKey := requestIDKey(requestID)
	buttons := approvalButtons(kind, requestKey, requestPayload)
	card := a.renderApprovalCard(sessionKey, sub, "等待审批", "orange", strings.TrimSpace(body), buttons)
	payload := map[string]any{}
	if strings.TrimSpace(body) != "" {
		payload["body"] = body
	}
	if len(requestPayload) > 0 {
		payload["request"] = requestPayload
	}
	err := a.deliverPendingCard(sub, card, pendingCardDelivery{
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
	a.replyCodexError(requestID, -32603, err.Error())
}

func (a *App) sendPermissionsCard(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any) {
	a.sendPermissionsCardWithPayload(requestID, threadID, turnID, itemID, body, permissions, nil)
}

func (a *App) sendPermissionsCardWithPayload(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any, requestPayload map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		a.replyCodexError(requestID, -32602, "no active session for permissions approval")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.renderApprovalCard(sessionKey, sub, "权限请求", "orange", strings.TrimSpace(body), []feishu.Button{
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
	err := a.deliverPendingCard(sub, card, pendingCardDelivery{
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
	a.replyCodexError(requestID, -32603, err.Error())
}

func (a *App) sendUserInputCard(requestID json.RawMessage, payload toolUserInputPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil || len(payload.Questions) == 0 {
		a.replyCodexError(requestID, -32602, "no active session for request_user_input")
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
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", prependAttentionMentionMarkdown(q.Question, sub.UserID), buttons)
	requestKey := requestIDKey(requestID)
	err := a.deliverPendingCard(sub, card, pendingCardDelivery{
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
	a.replyCodexError(requestID, -32603, err.Error())
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
