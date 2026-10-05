package feishuapp

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type PlanCardActionInputs struct {
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	Lifecycle           *frontendruntime.FrontendRuntime
	AsyncRunner         func(func())
	Context             func() context.Context
	FrontendID          string
	EffectRunner        frontendruntime.EffectRunner
	State               planmode.SessionStateProvider
	ReplyInThread       bool
	TransportAvailable  bool
}

func planCardActionHandlers(inputs PlanCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.plan": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if action == nil || strings.TrimSpace(action.MessageID) == "" {
				return inputs.CompleteMenuCommand(action, sessionKey, "/plan", "menu.tools")
			}
			messageID := strings.TrimSpace(action.MessageID)
			runAsync(inputs.Lifecycle, inputs.AsyncRunner, func() {
				resp, err := inputs.CompleteMenuCommand(action, sessionKey, "/plan", "menu.tools")
				if card := callbackResponseCard(resp); card != nil {
					patchMaintenanceCard(inputs.Context(), inputs.FrontendID, inputs.EffectRunner, messageID, card, "plan menu patch failed", "session_key", sessionKey, "message_id", messageID)
					return
				}
				text := callbackResponseToastText(resp)
				if err != nil {
					text = err.Error()
				}
				text = strings.TrimSpace(text)
				if text == "" || !inputs.TransportAvailable {
					return
				}
				if replyErr := newEffectOutbound(inputs.FrontendID, inputs.EffectRunner).ReplyText(context.Background(), messageID, text, actionReplyInThreadForSession(inputs.State.Session, sessionKey, inputs.ReplyInThread)); replyErr != nil {
					slog.Warn("plan async text reply failed", "session_key", sessionKey, "message_id", messageID, "error", replyErr)
				}
			})
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "正在处理 plan mode"}}, nil
		},
	}
}
