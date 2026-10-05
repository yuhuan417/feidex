package feishuapp

import (
	"context"
	"feidex/internal/textutil"
	"strings"

	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func callbackResponseCard(resp *callback.CardActionTriggerResponse) map[string]any {
	if resp == nil || resp.Card == nil {
		return nil
	}
	card, _ := resp.Card.Data.(map[string]any)
	return card
}

func callbackResponseToastText(resp *callback.CardActionTriggerResponse) string {
	if resp == nil || resp.Toast == nil {
		return ""
	}
	return strings.TrimSpace(resp.Toast.Content)
}

type AsyncCardActionInputs struct {
	Commands    MenuCommandService
	Lifecycle   *frontendruntime.FrontendRuntime
	AsyncRunner func(func())
	Actors      *frontendruntime.SessionActors
	Context     func() context.Context
	FrontendID  string
	Effects     frontendruntime.EffectRunner
}

type AsyncCardActionService struct {
	commands    MenuCommandService
	lifecycle   *frontendruntime.FrontendRuntime
	asyncRunner func(func())
	actors      *frontendruntime.SessionActors
	context     func() context.Context
	frontendID  string
	effects     frontendruntime.EffectRunner
}

func NewAsyncCardActionService(inputs AsyncCardActionInputs) AsyncCardActionService {
	return AsyncCardActionService{
		commands: inputs.Commands, lifecycle: inputs.Lifecycle, asyncRunner: inputs.AsyncRunner,
		actors: inputs.Actors, context: inputs.Context, frontendID: inputs.FrontendID, effects: inputs.Effects,
	}
}

func (s AsyncCardActionService) CompleteCommand(
	action *feishu.CardAction,
	sessionKey, rawCommand, fallbackAction, toastText string,
	preparingCard map[string]any,
	successCardFromText func(sessionKey, text string) map[string]any,
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return s.commands.Complete(action, sessionKey, rawCommand, fallbackAction)
	}
	messageID := strings.TrimSpace(action.MessageID)
	runSessionAsync(s.lifecycle, s.asyncRunner, s.actors, sessionKey, func() {
		text, card, err := s.commands.run(action, sessionKey, rawCommand)
		switch {
		case err != nil:
			card = failureCard(sessionKey, err.Error())
		case card != nil:
		case successCardFromText != nil:
			card = successCardFromText(sessionKey, strings.TrimSpace(text))
		default:
			card = failureCard(sessionKey, textutil.FirstNonEmpty(strings.TrimSpace(text), "命令没有返回卡片"))
		}
		ctx := context.Background()
		if s.context != nil {
			ctx = s.context()
		}
		patchMaintenanceCard(ctx, s.frontendID, s.effects, messageID, card, patchWarnMsg,
			"session_key", sessionKey,
			"message_id", messageID,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(preparingCard),
	}, nil
}

func (s AsyncCardActionService) CompleteRendered(
	action *feishu.CardAction,
	sessionKey, toastText string,
	preparingCard map[string]any,
	run func() (*callback.CardActionTriggerResponse, error),
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return run()
	}
	messageID := strings.TrimSpace(action.MessageID)
	runSessionAsync(s.lifecycle, s.asyncRunner, s.actors, sessionKey, func() {
		resp, err := run()
		card := callbackResponseCard(resp)
		if card == nil {
			errText := callbackResponseToastText(resp)
			if err != nil {
				errText = err.Error()
			}
			card = failureCard(sessionKey, textutil.FirstNonEmpty(strings.TrimSpace(errText), "操作没有返回卡片"))
		}
		ctx := context.Background()
		if s.context != nil {
			ctx = s.context()
		}
		patchMaintenanceCard(ctx, s.frontendID, s.effects, messageID, card, patchWarnMsg,
			"session_key", sessionKey,
			"message_id", messageID,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toastText},
		Card:  rawCard(preparingCard),
	}, nil
}
