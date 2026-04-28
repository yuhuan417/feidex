package serverrequest

import (
	"encoding/json"
	"log/slog"
	"strings"

	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// SendElicitationFormCard sends an MCP elicitation form card.
func (s *Service) SendElicitationFormCard(requestID json.RawMessage, payload ElicitationFormPayload) {
	sessionKey, sub := s.FindSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		s.ReplyCodexError(requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	card := s.SimpleStatusCard("需要补充表单", "orange", s.PrepareMentionText(pendingforms.RenderElicitationFormBody(payload), sub.UserID), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	err := s.DeliverPendingCard(sub, card, PendingCardDelivery{
		RequestKey:      requestKey,
		RequestIDStored: requestIDStored(requestID),
		Backend:         s.BackendCodex,
		Kind:            "mcp_elicitation_form",
		SessionKey:      sessionKey,
		ThreadID:        payload.ThreadID,
		TurnID:          payload.TurnID,
		OwnerUserID:     sub.UserID,
		PayloadJSON:     mustJSON(payload),
		WaitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		LinkKind:        "elicitation_form_card",
	})
	if err == nil {
		return
	}
	s.ReplyCodexError(requestID, -32603, err.Error())
}

// SendElicitationURLCard sends an MCP elicitation URL card.
func (s *Service) SendElicitationURLCard(requestID json.RawMessage, payload ElicitationURLPayload) {
	sessionKey, sub := s.FindSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		s.ReplyCodexError(requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	body := payload.Message
	if strings.TrimSpace(payload.URL) != "" {
		body += "\n\n打开链接：<" + payload.URL + ">"
	}
	card := s.SimpleStatusCard("外部表单", "orange", s.PrepareMentionText(body, sub.UserID), []feishu.Button{
		{Text: "已完成", Type: "primary", Value: map[string]any{"action": "elicitation_url.accept", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "elicitation_url.decline", "request_id": requestKey}},
		{Text: "取消", Type: "default", Value: map[string]any{"action": "elicitation_url.cancel", "request_id": requestKey}},
	})
	err := s.DeliverPendingCard(sub, card, PendingCardDelivery{
		RequestKey:      requestKey,
		RequestIDStored: requestIDStored(requestID),
		Backend:         s.BackendCodex,
		Kind:            "mcp_elicitation_url",
		SessionKey:      sessionKey,
		ThreadID:        payload.ThreadID,
		TurnID:          payload.TurnID,
		OwnerUserID:     sub.UserID,
		PayloadJSON:     mustJSON(payload),
		WaitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		LinkKind:        "elicitation_url_card",
	})
	if err == nil {
		return
	}
	s.ReplyCodexError(requestID, -32603, err.Error())
}

// CompleteElicitationURLAction handles accept/decline/cancel button clicks for elicitation URL cards.
func (s *Service) CompleteElicitationURLAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "mcp_elicitation_url" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	adapter := s.AdapterForPending(pending)
	decision, err := adapter.ReplyElicitationURL(pending, actionName)
	if err != nil {
		slog.Error("elicitation url reply failed",
			"backend", adapter.Kind(),
			"request_id", requestID,
			"action", actionName,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
		}, nil
	}
	_ = s.FinalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(s.SimpleStatusCard("已处理", "green", "已提交 "+decision+"。", nil)),
	}, nil
}

// CompleteElicitationFormText handles a text reply for an elicitation form.
func (s *Service) CompleteElicitationFormText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload ElicitationFormPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	adapter := s.AdapterForPending(pending)
	summary, err := adapter.ReplyElicitationForm(pending, payload, msg.Text)
	if err != nil {
		return err
	}
	_ = s.FinalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.PatchCard(pending.FeishuMsgID, s.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

// CompleteToolUserInputText handles a text reply for a tool user input form.
func (s *Service) CompleteToolUserInputText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload ToolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	adapter := s.AdapterForPending(pending)
	summary, err := adapter.ReplyTextUserInput(pending, payload, msg.Text)
	if err != nil {
		return err
	}
	_ = s.FinalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.PatchCard(pending.FeishuMsgID, s.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}
