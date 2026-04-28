package backend

import (
	"feidex/internal/app/appcore"
	appworkspace "feidex/internal/app/workspace"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type PermissionScope string

const (
	PermissionScopeGlobal       PermissionScope = "global"
	PermissionScopeWorkspace    PermissionScope = "workspace"
	PermissionScopeConversation PermissionScope = "conversation"
)

type ConversationCapabilities struct {
	Slash        string
	Noun         string
	SummaryLabel string
}

type PermissionCapabilities struct {
	Scopes []PermissionScope
}

type CapabilitySet struct {
	Kind         string
	Conversation ConversationCapabilities
	Permissions  PermissionCapabilities
}

type RuntimeDriver interface {
	DisplayName() string
	AutoRetryTitle() string
}

type WorkspaceThreadOps interface {
	EnsureCodexWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
	EnsureClaudeWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
	ListCodexWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	ListClaudeWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	StartCodexWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
	StartClaudeWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
}

type ConversationDriver interface {
	PrimarySlash() string
	Noun() string
	SummaryLabel() string
	WorkspaceSwitchInFlightNotice() string
	WorkspaceSwitchBindingFailureNotice() string
	WorkspaceSwitchBindingNotice(binding *appworkspace.ThreadBinding) string
	EnsureWorkspaceThreadBinding(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
	ListWorkspaceThreads(ops WorkspaceThreadOps, sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	StartWorkspaceThread(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error)
}

type PermissionApp interface {
	appcore.AppConfig
	Feishu() appcore.FeishuClient
}

type WorkspacePermissionCommandRequest struct {
	Message                             *feishu.InboundMessage
	Args                                []string
	SessionKey                          string
	CurrentWorkspace                    func(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace)
	ShowWorkspaceSandboxMenu            func(msg *feishu.InboundMessage) error
	ShowWorkspacePolicyMenu             func(msg *feishu.InboundMessage) error
	ShowWorkspacePermissionModeMenu     func(msg *feishu.InboundMessage) error
	CompleteWorkspaceSandboxSet         func(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePolicySet          func(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePermissionModeSet  func(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error)
	ReplyCommandActionResponse          func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
	CommandActionFromMessage            func(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction
}

type ConversationPermissionCommandRequest struct {
	Message                               *feishu.InboundMessage
	Args                                  []string
	SessionKey                            string
	CurrentThread                         func(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error)
	ShowConversationSandboxMenu           func(msg *feishu.InboundMessage) error
	ShowConversationPolicyMenu            func(msg *feishu.InboundMessage) error
	ShowConversationPermissionModeMenu    func(msg *feishu.InboundMessage) error
	CompleteConversationSandboxSet        func(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPolicySet         func(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPermissionModeSet func(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error)
	ReplyCommandActionResponse            func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
	CommandActionFromMessage              func(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction
}

type WorkspacePermissionRenderDeps struct {
	App            PermissionApp
	FormatMenuBody func(action, body string) string
}

type ConversationPermissionRenderDeps struct {
	App            PermissionApp
	Session        func(sessionKey string) *state.Session
	FormatMenuBody func(action, body string) string
	CommandLabel   func(label, slash string) string
}

type WorkspacePermissionUpdateDeps struct {
	UpdateWorkspaceDefaults func(workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error)
	RenderSandboxMenu       func(sessionKey string) (map[string]any, error)
	RenderPolicyMenu        func(sessionKey string) (map[string]any, error)
}

type WorkspacePermissionModeUpdateDeps struct {
	App                     PermissionApp
	Session                 func(sessionKey string) *state.Session
	UpdateWorkspaceDefaults func(workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error)
	ApplyRuntime            func(sessionKey, mode string) error
	RenderPermissionMenu    func(sessionKey string) (map[string]any, error)
}

type ConversationPermissionUpdateDeps struct {
	Session           func(sessionKey string) *state.Session
	SaveSession       func(sess *state.Session) error
	RenderSandboxMenu func(sessionKey string) (map[string]any, error)
	RenderPolicyMenu  func(sessionKey string) (map[string]any, error)
}

type ConversationPermissionModeUpdateDeps struct {
	App                  PermissionApp
	Session              func(sessionKey string) *state.Session
	SaveSession          func(sess *state.Session) error
	NormalizeRequested   func(raw string) (mode string, warning string, err error)
	ApplyRuntime         func(sessionKey, mode string) error
	RenderPermissionMenu func(sessionKey string) (map[string]any, error)
}

type PermissionDriver interface {
	SupportedScopes() []PermissionScope
	WorkspaceCommandUsage() string
	AppendWorkspaceSummaryLines(app PermissionApp, lines []string, currentWS *config.Workspace) []string
	WorkspaceConfigButtons(sessionKey string) []feishu.Button
	AppendStatusLines(app PermissionApp, lines []string, sess *state.Session, ws *config.Workspace) []string
	HandleWorkspaceCommand(req WorkspacePermissionCommandRequest) error
	RenderWorkspaceSandboxMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error)
	RenderWorkspacePolicyMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error)
	RenderWorkspacePermissionModeMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error)
	CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePolicySet(sessionKey, workspaceID, approvalPolicy string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePermissionModeSet(sessionKey, workspaceID, rawMode string, deps WorkspacePermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error)
	HandleConversationCommand(req ConversationPermissionCommandRequest) error
	RenderConversationSandboxMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	RenderConversationPolicyMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	RenderConversationPermissionModeMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPolicySet(sessionKey, threadID, approvalPolicy string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPermissionModeSet(sessionKey, threadID, rawMode string, deps ConversationPermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error)
}

type Driver interface {
	Kind() string
	Capabilities() CapabilitySet
	Runtime() RuntimeDriver
	Conversation() ConversationDriver
	Permission() PermissionDriver
}

func DriverForApp(app appcore.AppConfig) Driver {
	return DriverForKind(appcore.ConfiguredBackend(app))
}

func DriverForKind(kind string) Driver {
	switch appruntime.NormalizeBackend(kind) {
	case appruntime.BackendClaude:
		return claudeDriver{}
	default:
		return codexDriver{}
	}
}

type codexDriver struct{}
type claudeDriver struct{}

type backendRuntimeDriver struct {
	displayName string
	autoRetry   string
}

func (d backendRuntimeDriver) DisplayName() string { return d.displayName }
func (d backendRuntimeDriver) AutoRetryTitle() string {
	return d.autoRetry
}

