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
	card := pendingforms.RenderElicitationRequestCard(requestKey, payload, pendingforms.FormDrafts{}, sub.UserID).Card
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

func (s *Service) renderResolvedElicitationCard(pending *state.PendingRequest, payload ElicitationFormPayload, summary string) map[string]any {
	original := strings.TrimSpace(pendingforms.RenderElicitationFormBody(payload))
	lines := []string{"处理结果: 已提交"}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, strings.TrimSpace(summary))
	}
	if original != "" {
		lines = append(lines, "", original)
	}
	title := "表单已提交"
	workspaceID := ""
	if s.Session != nil && pending != nil {
		if sess := s.Session(strings.TrimSpace(pending.SessionKey)); sess != nil {
			workspaceID = strings.TrimSpace(sess.WorkspaceID)
		}
	}
	if s.ContentCardTitle != nil {
		title = s.ContentCardTitle(strings.TrimSpace(pending.SessionKey), workspaceID, title)
	}
	return s.SimpleStatusCard(title, "green", strings.Join(lines, "\n"), nil)
}

func (s *Service) renderElicitationDecisionResultCard(pending *state.PendingRequest, payload ElicitationFormPayload, statusLine, title, color string) map[string]any {
	lines := []string{"处理结果: " + strings.TrimSpace(statusLine)}
	if body := strings.TrimSpace(payload.Message); body != "" {
		lines = append(lines, "", body)
	}
	workspaceID := ""
	if s.Session != nil && pending != nil {
		if sess := s.Session(strings.TrimSpace(pending.SessionKey)); sess != nil {
			workspaceID = strings.TrimSpace(sess.WorkspaceID)
		}
	}
	if s.ContentCardTitle != nil {
		title = s.ContentCardTitle(strings.TrimSpace(pending.SessionKey), workspaceID, title)
	}
	return s.SimpleStatusCard(title, color, strings.Join(lines, "\n"), nil)
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
		_ = s.PatchCard(pending.FeishuMsgID, s.renderResolvedElicitationCard(pending, payload, summary))
	}
	return nil
}

// CompleteElicitationFormAnswer handles interactive elicitation form replies.
func (s *Service) CompleteElicitationFormAnswer(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action.ActionValue, "request_id")
	elicitationAction := actionStringValue(action.ActionValue, "elicitation_action")
	fieldName := actionStringValue(action.ActionValue, "field_name")
	answer := actionStringValue(action.ActionValue, "answer")
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}

	var payload ElicitationFormPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "表单内容已损坏"}}, nil
	}

	if strings.TrimSpace(elicitationAction) != "" {
		return s.completeElicitationDirectAction(action, pending, payload, elicitationAction)
	}
	if strings.TrimSpace(fieldName) != "" || strings.TrimSpace(answer) != "" {
		return s.completeElicitationQuickAnswer(action, pending, payload, fieldName, answer)
	}
	return s.completeElicitationFormSubmit(action, pending, payload)
}

func (s *Service) completeElicitationDirectAction(action *feishu.CardAction, pending *state.PendingRequest, payload ElicitationFormPayload, decision string) (*callback.CardActionTriggerResponse, error) {
	spec, err := pendingforms.ParseElicitationSchema(payload.Schema)
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "表单内容已损坏"},
		}, nil
	}
	if spec.RenderMode != pendingforms.ElicitationRenderModeConfirm {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前卡片与表单不匹配，请重试"},
		}, nil
	}
	adapter := s.AdapterForPending(pending)
	switch strings.TrimSpace(decision) {
	case "accept":
		if err := adapter.ReplyElicitationContent(pending, map[string]any{}); err != nil {
			slog.Error("elicitation confirm accept failed",
				"backend", adapter.Kind(),
				"request_id", strings.TrimSpace(pending.ID),
				"user_id", action.UserID,
				"error", err,
			)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			}, nil
		}
		_ = s.FinalizePendingReply(pending)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已允许"},
			Card:  rawCard(s.renderElicitationDecisionResultCard(pending, payload, "已允许", "表单已提交", "green")),
		}, nil
	case "decline":
		if err := adapter.ReplyElicitationAction(pending, "decline"); err != nil {
			slog.Error("elicitation confirm decline failed",
				"backend", adapter.Kind(),
				"request_id", strings.TrimSpace(pending.ID),
				"user_id", action.UserID,
				"error", err,
			)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			}, nil
		}
		_ = s.FinalizePendingReply(pending)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已拒绝"},
			Card:  rawCard(s.renderElicitationDecisionResultCard(pending, payload, "已拒绝", "表单已拒绝", "red")),
		}, nil
	default:
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "不支持的操作"},
		}, nil
	}
}

func (s *Service) completeElicitationQuickAnswer(action *feishu.CardAction, pending *state.PendingRequest, payload ElicitationFormPayload, fieldName, answer string) (*callback.CardActionTriggerResponse, error) {
	spec, err := pendingforms.ParseElicitationSchema(payload.Schema)
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "表单暂不支持快速提交"},
		}, nil
	}
	if len(spec.Fields) != 1 || strings.TrimSpace(spec.Fields[0].Name) != strings.TrimSpace(fieldName) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前卡片与表单不匹配，请重试"},
		}, nil
	}
	field := spec.Fields[0]
	content := map[string]any{}
	summary := ""
	if strings.TrimSpace(answer) != "__skip__" {
		var (
			value   any
			include bool
		)
		value, summary, include, err = pendingforms.NormalizeElicitationQuickAnswer(field, answer)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			}, nil
		}
		if include {
			content[field.Name] = value
		}
	}
	adapter := s.AdapterForPending(pending)
	if err := adapter.ReplyElicitationContent(pending, content); err != nil {
		slog.Error("elicitation quick reply failed",
			"backend", adapter.Kind(),
			"request_id", strings.TrimSpace(pending.ID),
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
		Card:  rawCard(s.renderResolvedElicitationCard(pending, payload, summary)),
	}, nil
}

func (s *Service) completeElicitationFormSubmit(action *feishu.CardAction, pending *state.PendingRequest, payload ElicitationFormPayload) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	drafts := pendingforms.ElicitationDraftsFromCardAction(payload, action)
	content, summary, err := pendingforms.BuildElicitationResponseFromDrafts(payload, drafts)
	renderCard := pendingforms.RenderElicitationRequestCard(requestID, payload, drafts, pending.OwnerUserID).Card
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(renderCard),
		}, nil
	}
	adapter := s.AdapterForPending(pending)
	if err := adapter.ReplyElicitationContent(pending, content); err != nil {
		slog.Error("elicitation form reply failed",
			"backend", adapter.Kind(),
			"request_id", requestID,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			Card:  rawCard(renderCard),
		}, nil
	}
	_ = s.FinalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(s.renderResolvedElicitationCard(pending, payload, summary)),
	}, nil
}

// CompleteElicitationMultiToggle handles button toggles for multi-select elicitation fields.
func (s *Service) CompleteElicitationMultiToggle(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action.ActionValue, "request_id")
	fieldName := actionStringValue(action.ActionValue, "field_name")
	optionValue := actionStringValue(action.ActionValue, "option_value")
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	var payload ElicitationFormPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "表单内容已损坏"}}, nil
	}
	drafts := pendingforms.ElicitationDraftsFromCardAction(payload, action)
	drafts = pendingforms.ToggleToolUserInputMultiDraft(drafts, fieldName, optionValue)
	return &callback.CardActionTriggerResponse{
		Card: rawCard(pendingforms.RenderElicitationRequestCard(requestID, payload, drafts, pending.OwnerUserID).Card),
	}, nil
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
		_ = s.PatchCard(pending.FeishuMsgID, s.renderResolvedUserInputCard(pending, payload, summary))
	}
	return nil
}
