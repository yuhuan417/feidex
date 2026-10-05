package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	appstate "feidex/internal/adapter/storage/json/scoped"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/application/interaction"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type WorkspaceCommandInputs struct {
	Runtime            BackendRuntimeDeps
	Store              *state.Store
	WorkspaceSelection workspaceapp.SelectionService
	Settings           workspaceapp.SettingsService
	Planning           *workspaceapp.PlanningService
	Workflow           *workspaceapp.Workflow
	Forms              *interaction.FormService
	FrontendID         string
	Effects            frontendruntime.EffectRunner
	Feishu             FeishuClient
	Presentation       *workspacecards.Presentation
}

func BuildWorkspaceCommandDependencies(inputs WorkspaceCommandInputs) workspacecmd.Dependencies {
	configProvider := newFrontendConfigProvider(inputs.Runtime, inputs.Store, inputs.WorkspaceSelection)
	backendDriver := appbackend.SelectedDriver{Selected: inputs.Runtime.view.configuredBackend}
	return workspacecmd.Dependencies{
		ConfigProvider: configProvider,
		Settings:       inputs.Settings,
		Planning:       inputs.Planning,
		Workflow:       inputs.Workflow,
		Forms:          inputs.Forms,
		Outbound:       newEffectOutbound(inputs.FrontendID, inputs.Effects),
		CardRenderer:   simpleStatusCardRenderer{client: inputs.Feishu},
		BotNameFn: func() string {
			if inputs.Feishu == nil {
				return ""
			}
			return inputs.Feishu.BotName()
		},
		ContextProvider:  configProvider,
		BackendDriver:    backendDriver,
		SettingsRenderer: inputs.Presentation,
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
type WorkspaceConfigurationInputs struct {
	Dependencies        workspacecmd.Dependencies
	State               *appstate.Store
	LiveThreads         *frontendruntime.LiveThreads
	Conversations       *conversationapp.Service
	Presentation        *workspacecards.Presentation
	CompleteMenuCommand workspacecmd.CompleteMenuCommandFn
	FrontendID          string
	Effects             frontendruntime.EffectRunner
	ReplyInThread       bool
}

func BuildWorkspaceConfigurationService(inputs WorkspaceConfigurationInputs) *workspacecmd.ConfigService {
	dependencies := inputs.Dependencies
	return workspacecmd.NewConfigService(workspacecmd.ConfigDeps{
		Dependencies: dependencies,
		State:        workspaceStateDeps(inputs.State),
		SessionContext: workspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			ClearSessionLiveThread: inputs.LiveThreads.Clear,
		},
		Threads: workspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*workspacecmd.ThreadBinding, error) {
				return inputs.Conversations.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
		},
		Backend: workspaceBackendConfigDeps(dependencies.BackendDriver),
		Actions: workspacecmd.ActionDeps{
			CompleteMenuCommand: inputs.CompleteMenuCommand,
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponseWith(inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, resp)
			},
			CommandActionFromMessage: commandActionFromMessage,
		},
		Formatting: workspacecmd.FormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Render: workspacecmd.ConfigRenderDeps{
			RenderMenuCard: func(sessionKey string) map[string]any {
				return inputs.Presentation.RenderWorkspaceMenuCard(sessionKey)
			},
			RenderChooseMenuCard: func(sessionKey string) map[string]any {
				return inputs.Presentation.RenderWorkspaceChooseCard(sessionKey)
			},
			RenderSandboxMenuCard: func(sessionKey string) (map[string]any, error) {
				return inputs.Presentation.RenderWorkspaceSandboxMenuCard(sessionKey)
			},
			RenderPolicyMenuCard: func(sessionKey string) (map[string]any, error) {
				return inputs.Presentation.RenderWorkspacePolicyMenuCard(sessionKey)
			},
			RenderMultiAgentMenuCard: func(sessionKey string) (map[string]any, error) {
				return inputs.Presentation.RenderWorkspaceMultiAgentMenuCard(sessionKey)
			},
			RenderDeleteMenuCard: func(sessionKey string) (map[string]any, error) {
				return inputs.Presentation.RenderWorkspaceDeleteMenuCard(sessionKey)
			},
			RenderDeleteConfirmCard: func(sessionKey, workspaceID string) (map[string]any, error) {
				return inputs.Presentation.RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return inputs.Presentation.RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
		},
	})
}

func currentWorkspaceForMessage(workspaceconfiguration *workspacecmd.ConfigService, msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace) {
	return workspaceconfiguration.CurrentWorkspaceForMessage(msg)
}

func currentThreadForMessage(workspaceConfiguration *workspacecmd.ConfigService, backend func() string, msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = currentWorkspaceForMessage(workspaceConfiguration, msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("%s", primaryConversationMissingLabel(backend()))
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func commandWorkspace(a *App, msg *feishu.InboundMessage, args []string) error {
	return a.bindings.WorkspaceConfiguration.CommandWorkspace(msg, args, a.bindings.WorkspaceManagement)
}
