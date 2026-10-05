package feishuapp

import (
	"context"

	appbackend "feidex/internal/adapter/feishu/backend"
	appstate "feidex/internal/adapter/storage/json/scoped"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type BackendActionInputs struct {
	State         *appstate.Store
	Feishu        FeishuClient
	Submissions   *appsubmission.SubmissionQueueService
	BindingScope  BindingScope
	Backend       func() string
	SessionKey    func(*feishu.InboundMessage) string
	MenuCommands  MenuCommandService
	AsyncActions  AsyncCardActionService
	FrontendID    identity.FrontendID
	Effects       frontendruntime.EffectRunner
	ReplyInThread func() bool
}

func BuildBackendActions(inputs BackendActionInputs) appbackend.ActionService {
	state, feishuClient, submissions := inputs.State, inputs.Feishu, inputs.Submissions
	bindingScope := inputs.BindingScope.scope
	return appbackend.NewActionService(appbackend.ActionDeps{
		Backend:    inputs.Backend,
		SessionKey: inputs.SessionKey,
		Commands: appbackend.ActionCommandDeps{
			CommandMessageFromAction: func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
				return commandMessageFromAction(bindingScope, action, sessionKey, rawCommand)
			},
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return inputs.MenuCommands.Complete(action, sessionKey, rawCommand, parentAction)
			},
			CompleteAsyncCommandAction: func(
				action *feishu.CardAction,
				sessionKey, rawCommand, fallbackAction, toastText string,
				preparingCard map[string]any,
				successCardFromText func(sessionKey, text string) map[string]any,
				failureCard func(sessionKey, errText string) map[string]any,
				patchWarnMsg string,
			) (*callback.CardActionTriggerResponse, error) {
				return inputs.AsyncActions.CompleteCommand(action, sessionKey, rawCommand, fallbackAction, toastText, preparingCard, successCardFromText, failureCard, patchWarnMsg)
			},
		},
		Render: appbackend.ActionRenderDeps{
			RenderInterruptPreparingCard: func(sessionKey, parentAction string) map[string]any {
				return renderInterruptPreparingCard(state, feishuClient, sessionKey, parentAction)
			},
			RenderInterruptResultCard: func(sessionKey, parentAction, text string) map[string]any {
				return renderInterruptResultCard(state, feishuClient, sessionKey, parentAction, text)
			},
			RenderInterruptFailedCard: func(sessionKey, parentAction, targetTurnID, errText string) map[string]any {
				return renderInterruptFailedCard(state, feishuClient, sessionKey, parentAction, targetTurnID, errText)
			},
		},
		Execution: appbackend.ActionExecutionDeps{
			EnqueueSubmission: func(msg *feishu.InboundMessage) error {
				return enqueueSubmissionWithSessionKey(submissions, msg, inputs.SessionKey(msg), false)
			},
			EnqueuePassthroughCommand: func(msg *feishu.InboundMessage, rawCommand string) error {
				return enqueuePassthroughCommand(submissions, inputs.SessionKey(msg), msg, rawCommand)
			},
			ReplyText: func(ctx context.Context, msgID, text string, inThread bool) error {
				return newEffectOutbound(string(inputs.FrontendID), inputs.Effects).ReplyText(ctx, msgID, text, inThread)
			},
			ReplyInThreadEnabled: func(chatType string) bool {
				return inputs.ReplyInThread != nil && inputs.ReplyInThread()
			},
		},
	})
}
