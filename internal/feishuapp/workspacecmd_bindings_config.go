package feishuapp

import (
	"context"
	appbackend "feidex/internal/adapter/feishu/backend"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	"feidex/internal/config"
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

func workspaceCommandApp(a *App) workspacecmd.Dependencies {
	if a == nil {
		return workspacecmd.Dependencies{}
	}
	return workspacecmd.Dependencies{
		ConfigProvider: a,
		Settings:       a.bindings.WorkspaceSettings,
		Planning:       a.bindings.WorkspacePlanning,
		Workflow:       a.bindings.WorkspaceWorkflow,
		Forms:          a.bindings.Forms,
		Outbound:       workspaceOutbound{app: a},
		CardRenderer:   workspaceCardRenderer{app: a},
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

// workspaceBackendConfigDeps reads the backend-specific workspace notices from
// the active driver at construction time. They are plain reads on the driver,
// so the workspace services do not have to hold the backend configuration
// service, which is assembled later in composition — reading it here would
// capture its zero value.
func workspaceBackendConfigDeps(driver appbackend.Driver) workspacecmd.BackendConfigDeps {
	if driver == nil {
		return workspacecmd.BackendConfigDeps{}
	}
	conversation := driver.Conversation()
	permission := driver.Permission()
	if conversation == nil || permission == nil {
		return workspacecmd.BackendConfigDeps{}
	}
	return workspacecmd.BackendConfigDeps{
		BackendWorkspaceSwitchBindingNotice:        conversation.WorkspaceSwitchBindingNotice,
		BackendWorkspaceSwitchBindingFailureNotice: conversation.WorkspaceSwitchBindingFailureNotice,
		BackendWorkspaceSwitchInFlightNotice:       conversation.WorkspaceSwitchInFlightNotice,
		BackendWorkspaceCommandUsage:               permission.WorkspaceCommandUsage,
	}
}

// buildWorkspaceConfigService takes the card presentation and conversation
// service as construction-time inputs. Reading them through a.bindings inside
// the closures would hide the dependency and force the caller to build this
// service before those bindings are assigned.
func buildWorkspaceConfigService(a *App, presentation *workspacecards.Presentation, conversations *conversationapp.Service) *workspacecmd.ConfigService {
	if a == nil {
		return workspacecmd.NewConfigService(workspacecmd.ConfigDeps{})
	}

	st := a.State()
	return workspacecmd.NewConfigService(workspacecmd.ConfigDeps{
		Dependencies: workspaceCommandApp(a),
		State:        workspaceStateDeps(st),
		SessionContext: workspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: workspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*workspacecmd.ThreadBinding, error) {
				return conversations.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
		},
		Backend: workspaceBackendConfigDeps(a.BackendDriver()),
		Actions: workspacecmd.ActionDeps{
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponse(a, msg, resp)
			},
			CommandActionFromMessage: commandActionFromMessage,
		},
		Formatting: workspacecmd.FormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Render: workspacecmd.ConfigRenderDeps{
			RenderMenuCard: func(sessionKey string) map[string]any {
				return presentation.RenderWorkspaceMenuCard(sessionKey)
			},
			RenderChooseMenuCard: func(sessionKey string) map[string]any {
				return presentation.RenderWorkspaceChooseCard(sessionKey)
			},
			RenderSandboxMenuCard: func(sessionKey string) (map[string]any, error) {
				return presentation.RenderWorkspaceSandboxMenuCard(sessionKey)
			},
			RenderPolicyMenuCard: func(sessionKey string) (map[string]any, error) {
				return presentation.RenderWorkspacePolicyMenuCard(sessionKey)
			},
			RenderMultiAgentMenuCard: func(sessionKey string) (map[string]any, error) {
				return presentation.RenderWorkspaceMultiAgentMenuCard(sessionKey)
			},
			RenderDeleteMenuCard: func(sessionKey string) (map[string]any, error) {
				return presentation.RenderWorkspaceDeleteMenuCard(sessionKey)
			},
			RenderDeleteConfirmCard: func(sessionKey, workspaceID string) (map[string]any, error) {
				return presentation.RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return presentation.RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
		},
	})
}

func currentWorkspaceForMessage(workspaceconfiguration *workspacecmd.ConfigService, msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace) {
	return workspaceconfiguration.CurrentWorkspaceForMessage(msg)
}

func currentThreadForMessage(a *App, msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = currentWorkspaceForMessage(a.bindings.WorkspaceConfiguration, msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("%s", primaryConversationMissingLabel(a.configView().configuredBackend()))
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func commandWorkspace(a *App, msg *feishu.InboundMessage, args []string) error {
	return a.bindings.WorkspaceConfiguration.CommandWorkspace(msg, args, a.bindings.WorkspaceManagement)
}
