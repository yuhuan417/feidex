package app

import (
	"encoding/json"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) completeUserInputAnswer(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID := actionStringValue(action, "request_id")
	questionID := actionStringValue(action, "question_id")
	answer := actionStringValue(action, "answer")
	pending := appState.pending(requestID)
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
		return a.completeUserInputQuickAnswer(action, pending, payload, questionID, answer)
	}
	return a.completeUserInputFormSubmit(action, pending, payload)
}

func (a *App) completeUserInputQuickAnswer(action *feishu.CardAction, pending *state.PendingRequest, payload toolUserInputPayload, questionID, answer string) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	selectionSummary := strings.TrimSpace(answer)
	if pendingBackend(a, pending) == backendClaude {
		answers, _, err := claudeAnswersFromSelections(payload, map[string]string{
			strings.TrimSpace(questionID): strings.TrimSpace(answer),
		})
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		if err := a.claude.ResolveUserInput(requestID, answers); err != nil {
			slog.Error("tool user input reply to Claude failed",
				"request_id", requestID,
				"user_id", action.UserID,
				"error", err,
			)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			}, nil
		}
		_ = a.appState().updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		a.resumeSubmissionAfterRequest(pending)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已提交"},
			Card:  rawCard(a.feishu.SimpleStatusCard("已提交", "green", selectionSummary, nil)),
		}, nil
	}

	replyPayload := map[string]any{
		"answers": map[string]any{
			questionID: map[string]any{
				"answers": []string{answer},
			},
		},
	}
	if err := a.codex.Reply(pendingRequestIDRaw(pending), replyPayload); err != nil {
		slog.Error("tool user input reply to codex failed",
			"request_id", requestID,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
		}, nil
	}
	_ = a.markPendingRequestReplied(requestID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已提交", "green", selectionSummary, nil)),
	}, nil
}

func (a *App) completeUserInputFormSubmit(action *feishu.CardAction, pending *state.PendingRequest, payload toolUserInputPayload) (*callback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(pending.ID)
	selections := toolUserInputSelectionsFromFormValues(payload, action.FormValue)
	if pendingBackend(a, pending) == backendClaude {
		answers, summary, err := claudeAnswersFromSelections(payload, selections)
		if err != nil {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: err.Error()},
				Card:  rawCard(renderToolUserInputFormCard(requestID, payload, selections)),
			}, nil
		}
		if err := a.claude.ResolveUserInput(requestID, answers); err != nil {
			slog.Error("tool user input reply to Claude failed",
				"request_id", requestID,
				"user_id", action.UserID,
				"error", err,
			)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
				Card:  rawCard(renderToolUserInputFormCard(requestID, payload, selections)),
			}, nil
		}
		_ = a.appState().updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		a.resumeSubmissionAfterRequest(pending)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已提交"},
			Card:  rawCard(a.feishu.SimpleStatusCard("已提交", "green", summary, nil)),
		}, nil
	}

	replyPayload, summary, err := buildToolUserInputResponseFromSelections(payload, selections)
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(renderToolUserInputFormCard(requestID, payload, selections)),
		}, nil
	}
	if err := a.codex.Reply(pendingRequestIDRaw(pending), replyPayload); err != nil {
		slog.Error("tool user input reply to codex failed",
			"request_id", requestID,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
			Card:  rawCard(renderToolUserInputFormCard(requestID, payload, selections)),
		}, nil
	}
	_ = a.markPendingRequestReplied(requestID)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已提交", "green", summary, nil)),
	}, nil
}
