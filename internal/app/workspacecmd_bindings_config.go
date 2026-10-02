package app

import (
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func workspaceCommandApp(a *App) appworkspacecmd.App {
	if a == nil {
		return appworkspacecmd.App{}
	}
	return appworkspacecmd.App{ConfigProvider: a, FeishuClient: a.feishu, ContextProvider: a, BackendDriver: a.BackendDriver()}
}

func newWorkspaceConfigService(a *App) *appworkspacecmd.ConfigService {

	st := a.State()
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewConfigService(appworkspacecmd.ConfigDeps{
		App:   workspaceCommandApp(a),
		State: workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			SwitchSessionWorkspace: conversation.SwitchSessionWorkspace,
			ClearSessionThreadCtx:  conversation.ClearThreadContext,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newConversationService(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
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
				return newWorkspaceRenderService(a).RenderWorkspaceMenuCard(sessionKey)
			},
			RenderChooseMenuCard: func(sessionKey string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceChooseCard(sessionKey)
			},
			RenderSandboxMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderService(a).RenderWorkspaceSandboxMenuCard(sessionKey)
			},
			RenderPolicyMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderService(a).RenderWorkspacePolicyMenuCard(sessionKey)
			},
			RenderMultiAgentMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderService(a).RenderWorkspaceMultiAgentMenuCard(sessionKey)
			},
			RenderDeleteMenuCard: func(sessionKey string) (map[string]any, error) {
				return newWorkspaceRenderService(a).RenderWorkspaceDeleteMenuCard(sessionKey)
			},
			RenderDeleteConfirmCard: func(sessionKey, workspaceID string) (map[string]any, error) {
				return newWorkspaceRenderService(a).RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
		},
	})
}

func currentWorkspaceForMessage(a *App, msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace) {
	return newWorkspaceConfigService(a).CurrentWorkspaceForMessage(msg)
}

func currentThreadForMessage(a *App, msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = currentWorkspaceForMessage(a, msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("%s", primaryConversationMissingLabel(configuredBackend(a)))
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func commandWorkspace(a *App, msg *feishu.InboundMessage, args []string) error {
	return newWorkspaceConfigService(a).CommandWorkspace(msg, args, newWorkspaceManagementService(a))
}
