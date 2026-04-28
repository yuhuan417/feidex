package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func sendElicitationFormCard(a *App, requestID json.RawMessage, payload elicitationFormPayload) {
	sessionKey, sub := findSubmissionByTurn(a, payload.ThreadID, payload.TurnID)
	if sub == nil {
		replyCodexError(a, requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.feishu.SimpleStatusCard("需要补充表单", "orange", prependAttentionMentionMarkdown(renderElicitationFormBody(payload), sub.UserID), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	err := deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "mcp_elicitation_form",
		sessionKey:      sessionKey,
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		linkKind:        "elicitation_form_card",
	})
	if err == nil {
		return
	}
	replyCodexError(a, requestID, -32603, err.Error())
}

func sendElicitationURLCard(a *App, requestID json.RawMessage, payload elicitationURLPayload) {
	sessionKey, sub := findSubmissionByTurn(a, payload.ThreadID, payload.TurnID)
	if sub == nil {
		replyCodexError(a, requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	body := payload.Message
	if strings.TrimSpace(payload.URL) != "" {
		body += "\n\n打开链接：<" + payload.URL + ">"
	}
	card := a.feishu.SimpleStatusCard("外部表单", "orange", prependAttentionMentionMarkdown(body, sub.UserID), []feishu.Button{
		{Text: "已完成", Type: "primary", Value: map[string]any{"action": "elicitation_url.accept", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "elicitation_url.decline", "request_id": requestKey}},
		{Text: "取消", Type: "default", Value: map[string]any{"action": "elicitation_url.cancel", "request_id": requestKey}},
	})
	err := deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "mcp_elicitation_url",
		sessionKey:      sessionKey,
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		linkKind:        "elicitation_url_card",
	})
	if err == nil {
		return
	}
	replyCodexError(a, requestID, -32603, err.Error())
}

func completeElicitationURLAction(a *App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.State()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.Pending(requestID)
	if pending == nil || pending.Kind != "mcp_elicitation_url" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	adapter := serverRequestAdapterForPending(a, pending)
	decision, err := adapter.replyElicitationURL(pending, actionName)
	if err != nil {
		slog.Error("elicitation url reply failed",
			"backend", adapter.kind(),
			"request_id", requestID,
			"action", actionName,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
		}, nil
	}
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已处理", "green", "已提交 "+decision+"。", nil)),
	}, nil
}

func (s pendingInputService) completeElicitationFormText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload elicitationFormPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	adapter := serverRequestAdapterForPending(s.app, pending)
	summary, err := adapter.replyElicitationForm(pending, payload, msg.Text)
	if err != nil {
		return err
	}
	_ = newRuntimeStateService(s.app).finalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, s.app.feishu.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

// Wrappers delegating to pendingforms.

var renderElicitationFormBody = pendingforms.RenderElicitationFormBody

var parseElicitationFormResponse = pendingforms.ParseElicitationFormResponse

var parseElicitationFieldValue = pendingforms.ParseElicitationFieldValue

var requiredSet = pendingforms.RequiredSet

var sortedMapKeys = pendingforms.SortedMapKeys

var displayFieldTitle = pendingforms.DisplayFieldTitle

var requiredMarker = pendingforms.RequiredMarker

var stringField = pendingforms.StringField

var fieldType = pendingforms.FieldType

var schemaOptionLabels = pendingforms.SchemaOptionLabels

var matchSchemaOption = pendingforms.MatchSchemaOption

var parseBool = pendingforms.ParseBool
