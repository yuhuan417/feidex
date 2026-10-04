package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func buildBackendConfigurationService(app *App) appbackend.ConfigurationService {
	driver := app.BackendDriver()

	inner := appbackend.NewConfigurationService(appbackend.ConfigurationDeps{
		Permissions: app,
		Driver:      driver,
		Formatting: appbackend.ConfigurationFormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Commands: appbackend.ConfigurationCommandDeps{
			HandleCodexModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return app.bindings.ModelCommands.CommandCodexModel(msg, args)
			},
			HandleClaudeModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return app.bindings.ModelCommands.CommandClaudeModel(msg, args)
			},
			HandleWorkspacePermissionCommand: func(msg *feishu.InboundMessage, args []string, sessionKey string) error {
				return driver.Permission().HandleWorkspaceCommand(appbackend.WorkspacePermissionCommandRequest{
					Message:    msg,
					Args:       args[1:],
					SessionKey: sessionKey,
					CurrentWorkspace: func(msg *feishu.InboundMessage) (string, *conversation.Session, *config.Workspace) {
						return currentWorkspaceForMessage(app.bindings.WorkspaceConfiguration, msg)
					},
					ShowWorkspaceSandboxMenu: func(msg *feishu.InboundMessage) error {
						return app.bindings.WorkspaceConfiguration.ShowWorkspaceSandboxMenu(msg)
					},
					ShowWorkspacePolicyMenu: func(msg *feishu.InboundMessage) error {
						return app.bindings.WorkspaceConfiguration.ShowWorkspacePolicyMenu(msg)
					},
					ShowWorkspacePermissionModeMenu: func(msg *feishu.InboundMessage) error {
						return showClaudeWorkspacePermissionMenu(app, msg)
					},
					CompleteWorkspaceSandboxSet: func(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
						return app.bindings.WorkspaceManagement.CompleteWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
					},
					CompleteWorkspacePolicySet: func(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
						return app.bindings.WorkspaceManagement.CompleteWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
					},
					CompleteWorkspacePermissionModeSet: func(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error) {
						return app.bindings.WorkspaceManagement.CompleteWorkspacePermissionModeSet(action, sessionKey, workspaceID, rawMode)
					},
					ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
						return replyCommandActionResponse(app, msg, resp)
					},
					CommandActionFromMessage: func(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
						return commandActionFromMessage(msg, actionValue)
					},
				})
			},
		},
		Claude: appbackend.ConfigurationClaudeDeps{
			CompleteModelSet: func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
				return app.bindings.ModelCommands.CompleteClaudeModelSet(action, modelID)
			},
			CompleteModelOptionAdd: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return app.bindings.ModelCommands.CompleteClaudeModelOptionAdd(action)
			},
			CompleteModelOptionRemove: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return app.bindings.ModelCommands.CompleteClaudeModelOptionRemove(action)
			},
			CompleteEffortSet: func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
				return app.bindings.ModelCommands.CompleteClaudeEffortSet(action, effort)
			},
		},
		Codex: appbackend.ConfigurationCodexDeps{
			CompleteCodexGlobalModelSet: func(action *feishu.CardAction, value string) (*callback.CardActionTriggerResponse, error) {
				return app.bindings.ModelCommands.CompleteCodexGlobalModelSet(action, value)
			},
			CompleteCodexGlobalReasoningEffortSet: func(action *feishu.CardAction, value string) (*callback.CardActionTriggerResponse, error) {
				return app.bindings.ModelCommands.CompleteCodexGlobalReasoningEffortSet(action, value)
			},

			FetchModelList:                   app.bindings.ModelCommands.FetchModelList,
			FetchPlanCollaborationModePreset: app.bindings.ModelCommands.FetchPlanCollaborationModePreset,
			RenderModelConfigCard:            app.bindings.ModelCommands.RenderModelConfigCard,
		},
	})
	return inner
}
