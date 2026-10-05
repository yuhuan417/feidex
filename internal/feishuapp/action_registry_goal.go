package feishuapp

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/adapter/feishu/goalcmd"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type GoalCardActionInputs struct {
	Commands           goalcmd.Service
	Lifecycle          *frontendruntime.FrontendRuntime
	AsyncRunner        func(func())
	Context            func() context.Context
	FrontendID         string
	EffectRunner       frontendruntime.EffectRunner
	State              planmode.SessionStateProvider
	ReplyInThread      bool
	TransportAvailable bool
}

func goalCardActionHandlers(inputs GoalCardActionInputs) map[string]cardActionPortHandler {
	handlers := map[string]cardActionPortHandler{
		"menu.goal": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if action == nil || strings.TrimSpace(action.MessageID) == "" {
				return inputs.Commands.CompleteMenuGoal(action, sessionKey)
			}
			return runGoalCardActionAsync(inputs, action, sessionKey, "正在处理 goal", "goal menu patch failed", func(commands goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
				return commands.CompleteMenuGoal(action, sessionKey)
			})
		},
	}
	for name, action := range map[string]func(goalcmd.Service, *feishu.CardAction) (*callback.CardActionTriggerResponse, error){
		"goal.pause": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalStatusAction(action, conversation.ThreadGoalStatusPaused)
		},
		"goal.resume": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalStatusAction(action, conversation.ThreadGoalStatusActive)
		},
		"goal.clear": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalClearAction(action)
		},
		"goal.edit": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalEditAction(action)
		},
		"goal.replace.confirm": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalReplaceConfirm(action)
		},
		"goal.replace.cancel": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalReplaceCancel(action)
		},
		"goal.edit.submit": func(commands goalcmd.Service, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return commands.CompleteGoalEditSubmit(action)
		},
	} {
		name, action := name, action
		handlers[name] = func(card *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return runGoalCardActionAsync(inputs, card, actionSessionKey(card), goalCardActionToast(name), "goal action patch failed", func(commands goalcmd.Service) (*callback.CardActionTriggerResponse, error) {
				return action(commands, card)
			})
		}
	}
	return handlers
}

func goalCardActionToast(name string) string {
	switch name {
	case "goal.pause", "goal.resume":
		return "正在更新 goal"
	case "goal.clear":
		return "正在清除 goal"
	case "goal.edit":
		return "正在打开 goal 编辑"
	case "goal.replace.confirm":
		return "正在替换 goal"
	case "goal.replace.cancel":
		return "正在保留当前 goal"
	case "goal.edit.submit":
		return "正在保存 goal"
	default:
		return "正在处理 goal"
	}
}

func runGoalCardActionAsync(inputs GoalCardActionInputs, action *feishu.CardAction, sessionKey, toastText, patchWarnMsg string, run func(goalcmd.Service) (*callback.CardActionTriggerResponse, error)) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return run(inputs.Commands)
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(inputs.Lifecycle, inputs.AsyncRunner, func() {
		resp, err := run(inputs.Commands)
		completeGoalAsyncResultWith(inputs, sessionKey, messageID, resp, err, patchWarnMsg)
	})
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: toastText}}, nil
}

func completeGoalAsyncResultWith(inputs GoalCardActionInputs, sessionKey, messageID string, resp *callback.CardActionTriggerResponse, err error, patchWarnMsg string) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	if card := callbackResponseCard(resp); card != nil {
		patchMaintenanceCard(goalCardActionContext(inputs), inputs.FrontendID, inputs.EffectRunner, messageID, card, patchWarnMsg,
			"session_key", sessionKey,
			"message_id", messageID,
		)
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
		slog.Warn("goal async text reply failed", "session_key", sessionKey, "message_id", messageID, "error", replyErr)
	}
}

func goalCardActionContext(inputs GoalCardActionInputs) context.Context {
	if inputs.Context != nil {
		if ctx := inputs.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}
