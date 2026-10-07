package feishuapp

import (
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ReviewCardActionInputs struct {
	Dependencies        appreviewcmd.Dependencies
	ReviewCommands      appreviewcmd.ReviewFormService
	Backend             func() string
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func reviewCardActionHandlers(inputs ReviewCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.review": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if !menuActionVisibleForBackend("menu.review", inputs.Backend()) {
				return inputs.CompleteMenuCommand(action, sessionKey, "/review", "menu.tools")
			}
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开代码审查"}, Card: rawCard(inputs.ReviewCommands.RenderReviewMenuCard(sessionKey))}, nil
		},
		"review.start.uncommitted": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return appreviewcmd.CompleteMenuReviewUncommitted(inputs.Dependencies, action, actionSessionKey(action))
		},
		"review.start.base": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return appreviewcmd.CompleteMenuReviewBase(inputs.Dependencies, action, actionSessionKey(action))
		},
		"review.start.commit": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return appreviewcmd.CompleteMenuReviewCommit(inputs.Dependencies, action, actionSessionKey(action))
		},
		"review.start.custom": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/review custom", "menu.review")
		},
	}
}
