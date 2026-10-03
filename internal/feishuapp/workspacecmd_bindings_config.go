package feishuapp

import (
	"context"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	configadapter "feidex/internal/adapter/config"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type workspaceOutbound struct{ app *App }

func (o workspaceOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return replyTextByAnchorEffect(ctx, o.app, messageID, text, inThread)
}
func (o workspaceOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}
func (o workspaceOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return patchCardEffect(ctx, o.app, messageID, card)
}

type workspaceCardRenderer struct{ app *App }

func (r workspaceCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

func workspaceCommandApp(a *App) appworkspacecmd.Dependencies {
	if a == nil {
		return appworkspacecmd.Dependencies{}
	}
	return appworkspacecmd.Dependencies{
		ConfigProvider: a,
		Lifecycle: &workspaceapp.Lifecycle{
			Frontend:      identity.FrontendID(a.FrontendID()),
			Selection:     a.WorkspaceSelection(),
			Configuration: workspaceapp.ConfigurationService{Repository: configadapter.NewWorkspaceRepository(a)},
			Repository:    configadapter.WorkspaceLifecycleRepository{Source: a, Scope: a.State()},
		},
		Outbound:     workspaceOutbound{app: a},
		CardRenderer: workspaceCardRenderer{app: a},
		BotNameFn: func() string {
			if a == nil || a.feishu == nil {
				return ""
			}
			return a.feishu.BotName()
		},
		ContextProvider:  a,
		BackendDriver:    a.BackendDriver(),
		SettingsRenderer: buildWorkspaceRenderService(a),
	}
}

func newWorkspaceConfigService(a *App) *appworkspacecmd.ConfigService {
	return compositionService(a, "workspaceConfig", func() *appworkspacecmd.ConfigService {
		return buildWorkspaceConfigService(a)
	})
}

func buildWorkspaceConfigService(a *App) *appworkspacecmd.ConfigService {
	if a == nil {
		return appworkspacecmd.NewConfigService(appworkspacecmd.ConfigDeps{})
	}

	st := a.State()
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewConfigService(appworkspacecmd.ConfigDeps{
		Dependencies: workspaceCommandApp(a),
		State:        workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newConversationService(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
		},
		Backend: appworkspacecmd.BackendConfigDeps{
			BackendWorkspaceSwitchBindingNotice:        bcfg.backendWorkspaceSwitchBindingNotice,
			BackendWorkspaceSwitchBindingFailureNotice: bcfg.backendWorkspaceSwitchBindingFailureNotice,
			BackendWorkspaceSwitchInFlightNotice:       bcfg.backendWorkspaceSwitchInFlightNotice,
			BackendWorkspaceCommandUsage:               bcfg.backendWorkspaceCommandUsage,
			BackendWorkspacePermissionCommand:          bcfg.handleBackendWorkspacePermissionCommand,
		},
		Actions: appworkspacecmd.ActionDeps{
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponse(a, msg, resp)
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
