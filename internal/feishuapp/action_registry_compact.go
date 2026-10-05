package feishuapp

import (
	"context"
	"strings"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/application/compaction"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type CompactCardActionInputs struct {
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	Actions             appbackend.ActionService
	Compaction          *compaction.Service
	State               planmode.SessionStateProvider
	Lifecycle           *frontendruntime.FrontendRuntime
	AsyncRunner         func(func())
	Context             context.Context
	FrontendID          string
	EffectRunner        frontendruntime.EffectRunner
}

func compactCardActionHandlers(inputs CompactCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{"menu.compact": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		sessionKey := actionSessionKey(action)
		messageID := ""
		userID := ""
		if action != nil {
			messageID, userID = strings.TrimSpace(action.MessageID), strings.TrimSpace(action.UserID)
		}
		if messageID == "" {
			return inputs.CompleteMenuCommand(action, sessionKey, "/compact", "menu.tools")
		}
		runAsync(inputs.Lifecycle, inputs.AsyncRunner, func() {
			card := renderCompactAcceptedCard(inputs.State, sessionKey)
			if err := inputs.Actions.RunMenuCompactAction(action, sessionKey, inputs.Compaction); err != nil {
				card = renderCompactFailedCard(inputs.State, sessionKey, err.Error())
			}
			patchMaintenanceCard(compactContext(inputs), inputs.FrontendID, inputs.EffectRunner, messageID, card, "compact menu patch failed", "session_key", sessionKey, "message_id", messageID, "user_id", userID)
		})
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在请求压缩当前线程上下文"}, Card: rawCard(renderCompactPreparingCard(inputs.State, sessionKey))}, nil
	}}
}

func compactContext(inputs CompactCardActionInputs) context.Context {
	if inputs.Context != nil {
		return inputs.Context
	}
	return context.Background()
}
