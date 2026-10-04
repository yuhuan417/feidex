package feishuapp

import (
	history "feidex/internal/adapter/feishu/history"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func historyCardActionHandlers(service history.Service) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"history.page": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			card, err := service.RenderHistoryCard(actionSessionKey(action), actionIntValue(action, "page"))
			if err != nil {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
			}
			return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
		},
		"history.detail": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			card, err := service.RenderHistoryDetailCard(actionSessionKey(action), actionIntValue(action, "index"))
			if err != nil {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
			}
			return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
		},
		"history.detail.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			errResp, index, ok := actionIndexOption(action, "未收到有效 turn 选项")
			if !ok {
				return errResp, nil
			}
			card, err := service.RenderHistoryDetailCard(actionSessionKey(action), index)
			if err != nil {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
			}
			return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
		},
	}
}
