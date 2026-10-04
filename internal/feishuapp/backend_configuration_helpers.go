package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func buildBackendConfigurationService(app *App) appbackend.ConfigurationService {
	driver := app.BackendDriver()
	// Model commands are assembled before the backend configuration service in
	// both composition entry points, so read them once instead of closing over
	// the aggregate.
	modelCommands := app.bindings.ModelCommands

	inner := appbackend.NewConfigurationService(appbackend.ConfigurationDeps{
		Permissions: app,
		Driver:      driver,
		Formatting: appbackend.ConfigurationFormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Commands: appbackend.ConfigurationCommandDeps{
			HandleCodexModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return modelCommands.CommandCodexModel(msg, args)
			},
			HandleClaudeModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return modelCommands.CommandClaudeModel(msg, args)
			},
		},
		Claude: appbackend.ConfigurationClaudeDeps{
			CompleteModelSet: func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeModelSet(action, modelID)
			},
			CompleteModelOptionAdd: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeModelOptionAdd(action)
			},
			CompleteModelOptionRemove: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeModelOptionRemove(action)
			},
			CompleteEffortSet: func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeEffortSet(action, effort)
			},
		},
		Codex: appbackend.ConfigurationCodexDeps{
			CompleteCodexGlobalModelSet: func(action *feishu.CardAction, value string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteCodexGlobalModelSet(action, value)
			},
			CompleteCodexGlobalReasoningEffortSet: func(action *feishu.CardAction, value string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteCodexGlobalReasoningEffortSet(action, value)
			},

			FetchModelList:                   modelCommands.FetchModelList,
			FetchPlanCollaborationModePreset: modelCommands.FetchPlanCollaborationModePreset,
			RenderModelConfigCard:            modelCommands.RenderModelConfigCard,
		},
	})
	return inner
}
