package feishuapp

import (
	"context"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func buildBackendActionService(app *App) appbackend.ActionService {
	if app == nil {
		return appbackend.ActionService{}
	}
	return appbackend.NewActionService(appbackend.ActionDeps{
		Backend:    func() string { return app.configView().configuredBackend() },
		SessionKey: func(msg *feishu.InboundMessage) string { return app.configView().makeSessionKey(msg) },
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
				return newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner)).ReplyText(ctx, msgID, text, inThread)
			},
			ReplyInThreadEnabled: func(chatType string) bool {
				return app.configView().replyInThreadEnabled()
			},
		},
	})
}
