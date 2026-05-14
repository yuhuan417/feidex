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

func (s *Service) renderResolvedUserInputCard(pending *state.PendingRequest, payload ToolUserInputPayload, summary string) map[string]any {
	original := ""
	switch {
	case len(payload.Questions) == 1 && strings.TrimSpace(pending.Kind) == "tool_request_user_input":
		original = strings.TrimSpace(pendingforms.RenderToolUserInputQuickBody(payload.Questions[0]))
	default:
		original = strings.TrimSpace(pendingforms.RenderToolUserInputBody(payload))
	}
	lines := []string{"处理结果: 已提交"}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, strings.TrimSpace(summary))
	}
	if original != "" {
		lines = append(lines, "", original)
	}
	title := "输入已提交"
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

// CompleteUserInputAnswer handles the user_input.answer card action.
func (s *Service) CompleteUserInputAnswer(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action.ActionValue, "request_id")
	questionID := actionStringValue(action.ActionValue, "question_id")
	answer := actionStringValue(action.ActionValue, "answer")
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}

	var payload ToolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "问题内容已损坏"}}, nil
	}

	if strings.TrimSpace(questionID) != "" || strings.TrimSpace(answer) != "" {
		return s.completeUserInputQuickAnswer(action, pending, payload, questionID, answer)
	}
	return s.completeUserInputFormSubmit(action, pending, payload)
}

func (s *Service) completeUserInputQuickAnswer(action *feishu.CardAction, pending *state.PendingRequest, payload ToolUserInputPayload, questionID, answer string) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	adapter := s.AdapterForPending(pending)
	selectionSummary, err := adapter.ReplyQuickUserInput(pending, payload, questionID, answer)
	if err != nil {
		if IsUIWarningError(err) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			}, nil
		}
		slog.Error("tool user input quick reply failed",
			"backend", adapter.Kind(),
			"request_id", requestID,
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
		Card:  rawCard(s.renderResolvedUserInputCard(pending, payload, selectionSummary)),
	}, nil
}

func (s *Service) completeUserInputFormSubmit(action *feishu.CardAction, pending *state.PendingRequest, payload ToolUserInputPayload) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	drafts := pendingforms.ToolUserInputDraftsFromCardAction(payload, action)
	selections := pendingforms.ToolUserInputSelectionsFromDrafts(payload, drafts)
	adapter := s.AdapterForPending(pending)
	summary, err := adapter.ReplyFormUserInput(pending, payload, selections)
	if err != nil {
		if IsUIWarningError(err) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(pendingforms.RenderToolUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
			}, nil
		}
		slog.Error("tool user input form reply failed",
			"backend", adapter.Kind(),
			"request_id", requestID,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			Card:  rawCard(pendingforms.RenderToolUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
		}, nil
	}
	_ = s.FinalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(s.renderResolvedUserInputCard(pending, payload, summary)),
	}, nil
}

// CompleteUserInputMultiToggle handles the user_input.toggle_multi card action.
func (s *Service) CompleteUserInputMultiToggle(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID := actionStringValue(action.ActionValue, "request_id")
	questionID := actionStringValue(action.ActionValue, "question_id")
	optionLabel := actionStringValue(action.ActionValue, "option_label")
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}
	var payload ToolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "问题内容已损坏"}}, nil
	}
	drafts := pendingforms.ToolUserInputDraftsFromCardAction(payload, action)
	drafts = pendingforms.ToggleToolUserInputMultiDraft(drafts, questionID, optionLabel)
	return &callback.CardActionTriggerResponse{
		Card: rawCard(pendingforms.RenderToolUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
	}, nil
}

// SendUserInputFormCard sends a tool user input form card to the user.
func (s *Service) SendUserInputFormCard(requestID json.RawMessage, payload ToolUserInputPayload) {
	sessionKey, sub := s.FindSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		s.ReplyCodexError(requestID, -32602, "no active session for request_user_input")
		return
	}
	requestKey := requestIDKey(requestID)
	card := pendingforms.RenderToolUserInputFormCard(requestKey, payload, ToolUserInputFormDrafts{}, sub.UserID)
	err := s.DeliverPendingCard(sub, card, PendingCardDelivery{
		RequestKey:      requestKey,
		RequestIDStored: requestIDStored(requestID),
		Backend:         s.BackendCodex,
		Kind:            "tool_request_user_input_form",
		SessionKey:      sessionKey,
		ThreadID:        payload.ThreadID,
		TurnID:          payload.TurnID,
		ItemID:          payload.ItemID,
		OwnerUserID:     sub.UserID,
		PayloadJSON:     mustJSON(payload),
		WaitingStatus:   state.SubmissionStatusWaitingUserInput.String(),
		LinkKind:        "user_input_card",
	})
	if err == nil {
		return
	}
	s.ReplyCodexError(requestID, -32603, err.Error())
}
