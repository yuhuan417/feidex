package app

import (
	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// backendConfigurationService wraps backend.ConfigurationService to preserve
// the lowercase method names used throughout app/.
type backendConfigurationService struct {
	app   *App
	inner appbackend.ConfigurationService
}

func newBackendConfigurationService(app *App) backendConfigurationService {
	inner := appbackend.NewConfigurationService(appbackend.ConfigurationDeps{
		App: app,
		Formatting: appbackend.ConfigurationFormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Commands: appbackend.ConfigurationCommandDeps{
			HandleCodexModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return newModelConfigService(app).commandCodexModel(msg, args)
			},
			HandleClaudeModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return newModelConfigService(app).commandClaudeModel(msg, args)
			},
			HandleWorkspacePermissionCommand: func(msg *feishu.InboundMessage, args []string, sessionKey string) error {
				return appbackend.DriverForApp(app).Permission().HandleWorkspaceCommand(appbackend.WorkspacePermissionCommandRequest{
					Message:    msg,
					Args:       args[1:],
					SessionKey: sessionKey,
					CurrentWorkspace: func(msg *feishu.InboundMessage) (string, *state.Session, *config.Workspace) {
						return currentWorkspaceForMessage(app, msg)
					},
					ShowWorkspaceSandboxMenu: func(msg *feishu.InboundMessage) error {
						return newWorkspaceConfigServiceInner(app).ShowWorkspaceSandboxMenu(msg)
					},
					ShowWorkspacePolicyMenu: func(msg *feishu.InboundMessage) error {
						return newWorkspaceConfigServiceInner(app).ShowWorkspacePolicyMenu(msg)
					},
					ShowWorkspacePermissionModeMenu: func(msg *feishu.InboundMessage) error {
						return showClaudeWorkspacePermissionMenu(app, msg)
					},
					CompleteWorkspaceSandboxSet: func(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
						return newWorkspaceManagementServiceInner(app).CompleteWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
					},
					CompleteWorkspacePolicySet: func(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
						return newWorkspaceManagementServiceInner(app).CompleteWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
					},
					CompleteWorkspacePermissionModeSet: func(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error) {
						return newWorkspaceManagementServiceInner(app).CompleteWorkspacePermissionModeSet(action, sessionKey, workspaceID, rawMode)
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
				return newModelConfigService(app).completeClaudeModelSet(action, modelID)
			},
			CompleteModelOptionAdd: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return newModelConfigService(app).completeClaudeModelOptionAdd(action)
			},
			CompleteModelOptionRemove: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return newModelConfigService(app).completeClaudeModelOptionRemove(action)
			},
			CompleteEffortSet: func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
				return newModelConfigService(app).completeClaudeEffortSet(action, effort)
			},
		},
		Codex: appbackend.ConfigurationCodexDeps{
			FetchModelList:                   newModelConfigService(app).fetchModelList,
			FetchPlanCollaborationModePreset: newModelConfigService(app).fetchPlanCollaborationModePreset,
			UpdateGlobalModelConfig:          newModelConfigService(app).updateGlobalModelConfig,
			RenderModelConfigCard:            newModelConfigService(app).renderModelConfigCard,
		},
	})
	return backendConfigurationService{app: app, inner: inner}
}

func (s backendConfigurationService) backendWorkspaceCommandUsage() string {
	return s.inner.BackendWorkspaceCommandUsage()
}

func (s backendConfigurationService) handleBackendModelCommand(msg *feishu.InboundMessage, args []string) error {
	return s.inner.HandleBackendModelCommand(msg, args)
}

func (s backendConfigurationService) handleBackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	return s.inner.HandleBackendWorkspacePermissionCommand(msg, args, sessionKey)
}

func (s backendConfigurationService) appendBackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	return s.inner.AppendBackendWorkspaceSummaryLines(lines, currentWS)
}

func (s backendConfigurationService) backendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	return s.inner.BackendWorkspaceConfigButtons(sessionKey)
}

func (s backendConfigurationService) backendWorkspaceSwitchInFlightNotice() string {
	return s.inner.BackendWorkspaceSwitchInFlightNotice()
}

func (s backendConfigurationService) backendWorkspaceSwitchBindingFailureNotice() string {
	return s.inner.BackendWorkspaceSwitchBindingFailureNotice()
}

func (s backendConfigurationService) backendWorkspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	return s.inner.BackendWorkspaceSwitchBindingNotice(binding)
}

func (s backendConfigurationService) renderModelMenuCard(sessionKey string) map[string]any {
	return s.inner.RenderModelMenuCard(sessionKey)
}

func (s backendConfigurationService) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteGlobalModelSet(action, modelID)
}

func (s backendConfigurationService) completeClaudeModelOptionAdd(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeModelOptionAdd(action)
}

func (s backendConfigurationService) completeClaudeModelOptionRemove(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteClaudeModelOptionRemove(action)
}

func (s backendConfigurationService) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteGlobalReasoningEffortSet(action, reasoningEffort)
}

func (s backendConfigurationService) statusCardBody(sess *state.Session) string {
	return s.inner.StatusCardBody(sess)
}
