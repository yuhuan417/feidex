package backend

import (
	"feidex/internal/application/workspace"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	"strings"
	"sync"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const workspaceCommandUsage = "/workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID] | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace sandbox [MODE] | /workspace policy [POLICY]"

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

type ConversationDriver interface {
	PrimarySlash() string
	Noun() string
	SummaryLabel() string
	WorkspaceSwitchInFlightNotice() string
	WorkspaceSwitchBindingFailureNotice() string
	WorkspaceSwitchBindingNotice(binding *conversation.ThreadBinding) string
}

type PermissionDependencies interface {
	Config() *config.Config
	ConfigMu() *sync.RWMutex
	Backend() string
	FrontendConfigIndex() int
	Store() *state.Store
	WorkspaceSelection() workspace.SelectionService
}

type WorkspacePermissionCommandRequest struct {
	Message                            *feishu.InboundMessage
	Args                               []string
	SessionKey                         string
	CurrentWorkspace                   func(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace)
	ShowWorkspaceSandboxMenu           func(msg *feishu.InboundMessage) error
	ShowWorkspacePolicyMenu            func(msg *feishu.InboundMessage) error
	ShowWorkspacePermissionModeMenu    func(msg *feishu.InboundMessage) error
	ShowWorkspaceMultiAgentMenu        func(msg *feishu.InboundMessage) error
	CompleteWorkspaceSandboxSet        func(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePolicySet         func(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePermissionModeSet func(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspaceMultiAgentSet     func(action *feishu.CardAction, sessionKey, workspaceID, mode string) (*callback.CardActionTriggerResponse, error)
	ReplyCommandActionResponse         func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
	CommandActionFromMessage           func(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction
}

type ConversationPermissionCommandRequest struct {
	Message                               *feishu.InboundMessage
	Args                                  []string
	SessionKey                            string
	CurrentThread                         func(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error)
	ShowConversationSandboxMenu           func(msg *feishu.InboundMessage) error
	ShowConversationPolicyMenu            func(msg *feishu.InboundMessage) error
	ShowConversationPermissionModeMenu    func(msg *feishu.InboundMessage) error
	ShowConversationMultiAgentMenu        func(msg *feishu.InboundMessage) error
	CompleteConversationSandboxSet        func(action *feishu.CardAction, sessionKey, threadID, sandboxMode string) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPolicySet         func(action *feishu.CardAction, sessionKey, threadID, approvalPolicy string) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPermissionModeSet func(action *feishu.CardAction, sessionKey, threadID, rawMode string) (*callback.CardActionTriggerResponse, error)
	CompleteConversationMultiAgentSet     func(action *feishu.CardAction, sessionKey, threadID, mode string) (*callback.CardActionTriggerResponse, error)
	ReplyCommandActionResponse            func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
	CommandActionFromMessage              func(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction
}

type ConversationPermissionRenderDeps struct {
	Permissions    PermissionDependencies
	Session        func(sessionKey string) *conversation.Session
	FormatMenuBody func(action, body string) string
}

type WorkspacePermissionUpdateDeps struct {
	Settings interface {
		Set(string, string, routing.Setting, string) error
	}
	RenderSandboxMenu    func(sessionKey string) (map[string]any, error)
	RenderPolicyMenu     func(sessionKey string) (map[string]any, error)
	RenderMultiAgentMenu func(sessionKey string) (map[string]any, error)
}

type WorkspacePermissionModeUpdateDeps struct {
	Permissions PermissionDependencies
	Session     func(sessionKey string) *conversation.Session
	Settings    interface {
		Set(string, string, routing.Setting, string) error
	}
	RenderPermissionMenu func(sessionKey string) (map[string]any, error)
}

type ConversationPermissionUpdateDeps struct {
	Session  func(sessionKey string) *conversation.Session
	Settings interface {
		Set(string, string, routing.Setting, string) (*conversation.Session, error)
	}
	RenderSandboxMenu    func(sessionKey string) (map[string]any, error)
	RenderPolicyMenu     func(sessionKey string) (map[string]any, error)
	RenderMultiAgentMenu func(sessionKey string) (map[string]any, error)
}

type ConversationPermissionModeUpdateDeps struct {
	Settings interface {
		Set(string, string, string, string, bool) (*conversation.Session, error)
	}
	MessageID            string
	Async                bool
	RenderPermissionMenu func(sessionKey string) (map[string]any, error)
}

type PermissionDriver interface {
	SupportedScopes() []PermissionScope
	WorkspaceCommandUsage() string
	AppendStatusLines(app PermissionDependencies, lines []string, sess *conversation.Session, ws *config.Workspace) []string
	HandleWorkspaceCommand(req WorkspacePermissionCommandRequest) error
	CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePolicySet(sessionKey, workspaceID, approvalPolicy string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspacePermissionModeSet(sessionKey, workspaceID, rawMode string, deps WorkspacePermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteWorkspaceMultiAgentSet(sessionKey, workspaceID, mode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	HandleConversationCommand(req ConversationPermissionCommandRequest) error
	RenderConversationSandboxMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	RenderConversationPolicyMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	RenderConversationPermissionModeMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPolicySet(sessionKey, threadID, approvalPolicy string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
	CompleteConversationPermissionModeSet(sessionKey, threadID, rawMode string, deps ConversationPermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error)
	RenderConversationMultiAgentMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error)
	CompleteConversationMultiAgentSet(sessionKey, threadID, mode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error)
}

type Driver interface {
	Kind() string
	Capabilities() CapabilitySet
	Runtime() RuntimeDriver
	Conversation() ConversationDriver
	Permission() PermissionDriver
}

func DriverForKind(kind string) Driver {
	switch domainbackend.NormalizeBackend(kind) {
	case domainbackend.BackendCodex:
		return codexDriver{}
	case domainbackend.BackendClaude:
		return claudeDriver{}
	default:
		return unsupportedDriver{rawKind: strings.TrimSpace(kind)}
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
