package app

import (
	"context"

	appbackend "feidex/internal/app/backend"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newBackendActionService(app *App) appbackend.ActionService {
	return appbackend.NewActionService(appbackend.ActionDeps{
		App: app,
		Commands: appbackend.ActionCommandDeps{
			CommandMessageFromAction: func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
				return commandMessageFromAction(app, action, sessionKey, rawCommand)
			},
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return completeMenuCommand(app, action, sessionKey, rawCommand, parentAction)
			},
			CompleteAsyncCommandAction: func(
				action *feishu.CardAction,
				sessionKey, rawCommand, fallbackAction, toastText string,
				preparingCard map[string]any,
				successCardFromText func(sessionKey, text string) map[string]any,
				failureCard func(sessionKey, errText string) map[string]any,
				patchWarnMsg string,
			) (*callback.CardActionTriggerResponse, error) {
				return completeAsyncCommandAction(app, action, sessionKey, rawCommand, fallbackAction, toastText, preparingCard, successCardFromText, failureCard, patchWarnMsg)
			},
		},
		Render: appbackend.ActionRenderDeps{
			RenderInterruptPreparingCard: func(sessionKey, parentAction string) map[string]any {
				return renderInterruptPreparingCard(app, sessionKey, parentAction)
			},
			RenderInterruptResultCard: func(sessionKey, parentAction, text string) map[string]any {
				return renderInterruptResultCard(app, sessionKey, parentAction, text)
			},
			RenderInterruptFailedCard: func(sessionKey, parentAction, targetTurnID, errText string) map[string]any {
				return renderInterruptFailedCard(app, sessionKey, parentAction, targetTurnID, errText)
			},
		},
		Execution: appbackend.ActionExecutionDeps{
			EnqueueSubmission: func(msg *feishu.InboundMessage) error {
				return enqueueSubmission(app, msg)
			},
			EnqueuePassthroughCommand: func(msg *feishu.InboundMessage, rawCommand string) error {
				return enqueuePassthroughCommand(app, msg, rawCommand)
			},
			ReplyText: func(ctx context.Context, msgID, text string, inThread bool) error {
				return app.feishu.ReplyText(ctx, msgID, text, inThread)
			},
			ReplyInThreadEnabled: func(chatType string) bool {
				return replyInThreadEnabled(app, chatType)
			},
		},
	})
}
