package app

import (
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newWorkspaceConfigServiceInner(a *App) *appworkspacecmd.ConfigService {
	st := a.State()
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewConfigService(appworkspacecmd.ConfigDeps{
		App:   a,
		State: workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     sessionHasInFlightSubmission,
			SwitchSessionWorkspace: switchSessionWorkspace,
			ClearSessionThreadCtx:  clearSessionThreadContext,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newWorkspaceThreadServiceInner(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
		},
		Backend: appworkspacecmd.BackendConfigDeps{
			BackendWorkspaceSummaryLines:               bcfg.appendBackendWorkspaceSummaryLines,
			BackendWorkspaceConfigButtons:              bcfg.backendWorkspaceConfigButtons,
			BackendWorkspaceSwitchBindingNotice:        bcfg.backendWorkspaceSwitchBindingNotice,
			BackendWorkspaceSwitchBindingFailureNotice: bcfg.backendWorkspaceSwitchBindingFailureNotice,
			BackendWorkspaceSwitchInFlightNotice:       bcfg.backendWorkspaceSwitchInFlightNotice,
			BackendWorkspaceCommandUsage:               bcfg.backendWorkspaceCommandUsage,
			BackendWorkspacePermissionCommand:          bcfg.handleBackendWorkspacePermissionCommand,
		},
		Actions: appworkspacecmd.ActionDeps{
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return indirectCompleteMenuCommand(a, action, sessionKey, rawCommand, parentAction)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return indirectReplyCommandActionResponse(a, msg, resp)
			},
			CommandActionFromMessage: commandActionFromMessage,
		},
		Formatting: appworkspacecmd.FormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Render: appworkspacecmd.ConfigRenderDeps{
			RenderMenuCard: func(sessionKey string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(sessionKey)
			},
			RenderChooseMenuCard: func(sessionKey string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceChooseCard(sessionKey)
			},
			RenderSandboxMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceSandboxMenuCard(sessionKey)
			},
			RenderPolicyMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderServiceInner(a).RenderWorkspacePolicyMenuCard(sessionKey)
			},
			RenderDeleteMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceDeleteMenuCard(sessionKey)
			},
			RenderDeleteConfirmCard: func(sessionKey, workspaceID string) (map[string]any, error) {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
		},
	})
}
