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
	requestID, _ := action.ActionValue["request_id"].(string)
	questionID, _ := action.ActionValue["question_id"].(string)
	answer, _ := action.ActionValue["answer"].(string)
	pending := appState.pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限回答这个问题"}}, nil
	}
	if pendingBackend(a, pending) == backendClaude {
		var payload toolUserInputPayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "问题内容已损坏"}}, nil
		}
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
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		a.resumeSubmissionAfterRequest(pending)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已提交"},
			Card: &callback.Card{
				Type: "raw",
				Data: a.feishu.SimpleStatusCard("已提交", "green", answer, nil),
			},
		}, nil
	}
	payload := map[string]any{
		"answers": map[string]any{
			questionID: map[string]any{
				"answers": []string{answer},
			},
		},
	}
	if err := a.codex.Reply(pendingRequestIDRaw(pending), payload); err != nil {
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
		Card: &callback.Card{
			Type: "raw",
			Data: a.feishu.SimpleStatusCard("已提交", "green", answer, nil),
		},
	}, nil
}
