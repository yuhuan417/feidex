package app

import (
	"encoding/json"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func completeUserInputAnswer(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.State()
	requestID := actionStringValue(action, "request_id")
	questionID := actionStringValue(action, "question_id")
	answer := actionStringValue(action, "answer")
	pending := appState.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}

	var payload toolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "问题内容已损坏"}}, nil
	}

	if strings.TrimSpace(questionID) != "" || strings.TrimSpace(answer) != "" {
		return completeUserInputQuickAnswer(a, action, pending, payload, questionID, answer)
	}
	return completeUserInputFormSubmit(a, action, pending, payload)
}

func completeUserInputQuickAnswer(a *App, action *feishu.CardAction, pending *state.PendingRequest, payload toolUserInputPayload, questionID, answer string) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	adapter := serverRequestAdapterForPending(a, pending)
	selectionSummary, err := adapter.replyQuickUserInput(pending, payload, questionID, answer)
	if err != nil {
		if isUIWarningError(err) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			}, nil
		}
		slog.Error("tool user input quick reply failed",
			"backend", adapter.kind(),
			"request_id", requestID,
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
		Card:  rawCard(a.feishu.SimpleStatusCard("已提交", "green", selectionSummary, nil)),
	}, nil
}

func completeUserInputFormSubmit(a *App, action *feishu.CardAction, pending *state.PendingRequest, payload toolUserInputPayload) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	drafts := toolUserInputDraftsFromCardAction(payload, action)
	selections := toolUserInputSelectionsFromDrafts(payload, drafts)
	adapter := serverRequestAdapterForPending(a, pending)
	summary, err := adapter.replyFormUserInput(pending, payload, selections)
	if err != nil {
		if isUIWarningError(err) {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(renderToolUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
			}, nil
		}
		slog.Error("tool user input form reply failed",
			"backend", adapter.kind(),
			"request_id", requestID,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			Card:  rawCard(renderToolUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
		}, nil
	}
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已提交", "green", summary, nil)),
	}, nil
}

func completeUserInputMultiToggle(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.State()
	requestID := actionStringValue(action, "request_id")
	questionID := actionStringValue(action, "question_id")
	optionLabel := actionStringValue(action, "option_label")
	pending := appState.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}
	var payload toolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "问题内容已损坏"}}, nil
	}
	drafts := toolUserInputDraftsFromCardAction(payload, action)
	drafts = toggleToolUserInputMultiDraft(drafts, questionID, optionLabel)
	return &callback.CardActionTriggerResponse{
		Card: rawCard(renderToolUserInputFormCard(requestID, payload, drafts, pending.OwnerUserID)),
	}, nil
}
