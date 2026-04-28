package app

import (
	"fmt"
	"strings"

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

func currentWorkspaceForMessage(a *App, msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	return newWorkspaceConfigServiceInner(a).CurrentWorkspaceForMessage(msg)
}

func currentThreadForMessage(a *App, msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = currentWorkspaceForMessage(a, msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("%s", primaryConversationMissingLabel(configuredBackend(a)))
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func commandWorkspace(a *App, msg *feishu.InboundMessage, args []string) error {
	return newWorkspaceConfigServiceInner(a).CommandWorkspace(msg, args, newWorkspaceManagementServiceInner(a))
}
