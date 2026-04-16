package app

import (
	"log/slog"

	"feidex/internal/feishu"

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
