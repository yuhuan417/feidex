// Package workspacecmd provides workspace configuration, creation, deletion,
// and thread binding services extracted from the app god package.
package workspacecmd

import (
	"context"

	"feidex/internal/app/appcore"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Type aliases from the workspace sub-package.
type (
	SettingOption               = appworkspace.SettingOption
	NewPayload                  = appworkspace.NewPayload
	ClonePayload                = appworkspace.ClonePayload
	CloneTakeoverError          = appworkspace.CloneTakeoverError
	CloneExistingDirError       = appworkspace.CloneExistingDirError
	CloneExistingWorkspaceError = appworkspace.CloneExistingWorkspaceError
	CloneProgressSnapshot       = appworkspace.CloneProgressSnapshot
	ClonePlan                   = appworkspace.ClonePlan
	CloneProgressReporter       = appworkspace.CloneProgressReporter
	CloneOperation              = appworkspace.CloneOperation
	CloneTracker                = appworkspace.CloneTracker
	ThreadBinding               = appworkspace.ThreadBinding
	PathPickerPayload           = appworkspace.PathPickerPayload
)

// Constants from the workspace sub-package.
const (
	CommandUsage            = appworkspace.CommandUsage
	CloneProgressKeepLines  = appworkspace.CloneProgressKeepLines
	ClonePatchInterval      = appworkspace.ClonePatchInterval
	PathPickerKind          = appworkspace.PathPickerKind
	PathPickerModeDirectory = appworkspace.PathPickerModeDirectory
	PathPickerModeFile      = appworkspace.PathPickerModeFile
	PathPickerStyleDropdown = appworkspace.PathPickerStyleDropdown
)

// Var aliases from the workspace sub-package.
var (
	SandboxOptions               = appworkspace.SandboxOptions
	ApprovalPolicyOptions        = appworkspace.ApprovalPolicyOptions
	ParseCloneArgs               = appworkspace.ParseCloneArgs
	NewPayloadFromPending        = appworkspace.NewPayloadFromPending
	ClonePayloadFromPending      = appworkspace.ClonePayloadFromPending
	MergeNewFormValues           = appworkspace.MergeNewFormValues
	MergeCloneFormValues         = appworkspace.MergeCloneFormValues
	NewTakeoverPayload           = appworkspace.NewTakeoverPayload
	NewTakeoverPayloadWithNotice = appworkspace.NewTakeoverPayloadWithNotice
	NewExistingWorkspaceNotice   = appworkspace.NewExistingWorkspaceNotice
	NewTakeoverNotice            = appworkspace.NewTakeoverNotice
	SessionReferencesWorkspace   = appworkspace.SessionReferencesWorkspace
	SortThreadsByUpdated         = appworkspace.SortThreadsByUpdated
	CloneRepoName                = appworkspace.CloneRepoName
	CloneDefaultID               = appworkspace.CloneDefaultID
	GitClone                     = appworkspace.GitClone
	NewCloneTracker              = appworkspace.NewCloneTracker
	NewCloneOperation            = appworkspace.NewCloneOperation
	ReadCloneOutput              = appworkspace.ReadCloneOutput
)

// ---------------------------------------------------------------------------
// App interface
// ---------------------------------------------------------------------------

// App provides config, state, and Feishu client access. workspacecmd uses
// appcore helpers (DefaultWorkspaceID, ConfiguredBackend, MakeSessionKey,
// ReplyInThreadEnabled, FirstNonEmpty) which all accept this interface.
type App interface {
	appcore.AppExtended
	Feishu() appcore.FeishuClient
}

// ---------------------------------------------------------------------------
// CodexClient
// ---------------------------------------------------------------------------

// CodexClient is the narrow interface for Codex RPC operations.
type CodexClient interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// ---------------------------------------------------------------------------
// Callback function types
// ---------------------------------------------------------------------------

// State access callbacks.
type (
	GetSessionFn    func(key string) *state.Session
	SessionsFn      func() []*state.Session
	SaveSessionFn   func(sess *state.Session) error
	NextLocalIDFn   func(prefix string) (string, error)
	PendingFn       func(id string) *state.PendingRequest
	SavePendingFn   func(req *state.PendingRequest) error
	UpdatePendingFn func(id string, mutate func(*state.PendingRequest)) error
)

// Thread callbacks.
type (
	ListWorkspaceThreadsFn         func(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	EnsureWorkspaceThreadBindingFn func(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	StartWorkspaceThreadFn         func(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error)
	MarkSessionThreadLiveFn        func(sessionKey, threadID string)
	ClearSessionLiveThreadFn       func(sessionKey string)
)

// Session context callbacks.
type (
	SessionHasInFlightFn     func(sess *state.Session) bool
	SwitchSessionWorkspaceFn func(sess *state.Session, workspaceID string)
	ClearSessionThreadCtxFn  func(sess *state.Session)
	SetSessionThreadCtxFn    func(sess *state.Session, workspaceID, threadID, name, preview string)
	SessionResetActiveOpsFn  func(sess *state.Session)
)

// Clone operation callbacks.
type (
	SetCloneOpFn   func(requestID string, op *CloneOperation)
	GetCloneOpFn   func(requestID string) *CloneOperation
	ClearCloneOpFn func(requestID string)
	GitCloneFn     func(ctx context.Context, repoURL, targetDir string, report CloneProgressReporter) error
)

// Codex client callbacks.
type (
	RequireCodexClientFn     func() (CodexClient, error)
	BuildThreadStartParamsFn func(ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any
)

// Backend configuration callbacks.
type (
	BackendWorkspaceSummaryLinesFn               func(lines []string, currentWS *config.Workspace) []string
	BackendWorkspaceConfigButtonsFn              func(sessionKey string) []feishu.Button
	BackendWorkspaceSwitchBindingNoticeFn        func(binding *ThreadBinding) string
	BackendWorkspaceSwitchBindingFailureNoticeFn func() string
	BackendWorkspaceSwitchInFlightNoticeFn       func() string
	BackendWorkspaceCommandUsageFn               func() string
	BackendWorkspacePermissionCommandFn          func(msg *feishu.InboundMessage, args []string, sessionKey string) error
)

// Action handling callbacks.
type (
	CompleteMenuCommandFn        func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error)
	ReplyCommandActionResponseFn func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
	CommandActionFromMessageFn   func(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction
	CommandMessageFromActionFn   func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage
)

// Formatting callbacks.
type FormatMenuBodyFn func(action, body string) string

// Render callbacks.
type (
	RenderWorkspaceMenuCardFn               func(sessionKey string) map[string]any
	RenderWorkspaceSandboxMenuCardFn        func(sessionKey string) (map[string]any, error)
	RenderWorkspacePolicyMenuCardFn         func(sessionKey string) (map[string]any, error)
	RenderWorkspaceDeleteMenuCardFn         func(sessionKey string) (map[string]any, error)
	RenderWorkspaceDeleteConfirmCardFn      func(sessionKey, workspaceID string) (map[string]any, error)
)

// ---------------------------------------------------------------------------
// ConfigService
// ---------------------------------------------------------------------------

// ConfigService handles workspace listing, configuration menus (sandbox,
// policy), and workspace deletion.
type ConfigService struct {
	App App

	// State callbacks
	GetSession    GetSessionFn
	Sessions      SessionsFn
	SaveSession   SaveSessionFn
	NextLocalID   NextLocalIDFn
	Pending       PendingFn
	SavePending   SavePendingFn
	UpdatePending UpdatePendingFn

	// Session context callbacks
	SessionHasInFlight     SessionHasInFlightFn
	SwitchSessionWorkspace SwitchSessionWorkspaceFn
	ClearSessionThreadCtx  ClearSessionThreadCtxFn
	ClearSessionLiveThread ClearSessionLiveThreadFn

	// Thread callbacks
	EnsureWorkspaceThreadBinding EnsureWorkspaceThreadBindingFn

	// Backend config callbacks
	BackendWorkspaceSummaryLines               BackendWorkspaceSummaryLinesFn
	BackendWorkspaceConfigButtons              BackendWorkspaceConfigButtonsFn
	BackendWorkspaceSwitchBindingNotice        BackendWorkspaceSwitchBindingNoticeFn
	BackendWorkspaceSwitchBindingFailureNotice BackendWorkspaceSwitchBindingFailureNoticeFn
	BackendWorkspaceSwitchInFlightNotice       BackendWorkspaceSwitchInFlightNoticeFn
	BackendWorkspaceCommandUsage               BackendWorkspaceCommandUsageFn
	BackendWorkspacePermissionCommand          BackendWorkspacePermissionCommandFn

	// Action callbacks
	CompleteMenuCommand        CompleteMenuCommandFn
	ReplyCommandActionResponse ReplyCommandActionResponseFn
	CommandActionFromMessage   CommandActionFromMessageFn

	// Formatting
	FormatMenuBody FormatMenuBodyFn

	// Render callbacks
	RenderMenuCard                  RenderWorkspaceMenuCardFn
	RenderSandboxMenuCard           RenderWorkspaceSandboxMenuCardFn
	RenderPolicyMenuCard            RenderWorkspacePolicyMenuCardFn
	RenderDeleteMenuCard            RenderWorkspaceDeleteMenuCardFn
	RenderDeleteConfirmCard         RenderWorkspaceDeleteConfirmCardFn
	RenderCloneSwitchExistingCard   func(sessionKey, workspaceID, targetDir string) map[string]any
}

// ---------------------------------------------------------------------------
// ManagementService
// ---------------------------------------------------------------------------

// ManagementService handles workspace creation, cloning, and workspace
// use/switch operations.
type ManagementService struct {
	App App

	// State callbacks
	GetSession    GetSessionFn
	Sessions      SessionsFn
	SaveSession   SaveSessionFn
	NextLocalID   NextLocalIDFn
	Pending       PendingFn
	SavePending   SavePendingFn
	UpdatePending UpdatePendingFn

	// Session context callbacks
	SessionHasInFlight     SessionHasInFlightFn
	SwitchSessionWorkspace SwitchSessionWorkspaceFn
	ClearSessionThreadCtx  ClearSessionThreadCtxFn
	SetSessionThreadCtx    SetSessionThreadCtxFn
	SessionResetActiveOps  SessionResetActiveOpsFn

	// Thread callbacks
	EnsureWorkspaceThreadBinding EnsureWorkspaceThreadBindingFn
	MarkSessionThreadLive        MarkSessionThreadLiveFn
	ClearSessionLiveThread       ClearSessionLiveThreadFn
	StartWorkspaceThread         StartWorkspaceThreadFn

	// Clone operation callbacks
	SetCloneOp   SetCloneOpFn
	GetCloneOp   GetCloneOpFn
	ClearCloneOp ClearCloneOpFn
	GitClone     GitCloneFn

	// Codex client callbacks
	RequireCodexClient     RequireCodexClientFn
	BuildThreadStartParams BuildThreadStartParamsFn

	// Backend config callbacks
	BackendWorkspaceSwitchBindingNotice        BackendWorkspaceSwitchBindingNoticeFn
	BackendWorkspaceSwitchBindingFailureNotice BackendWorkspaceSwitchBindingFailureNoticeFn
	BackendWorkspaceSwitchInFlightNotice       BackendWorkspaceSwitchInFlightNoticeFn
	BackendWorkspaceCommandUsage               BackendWorkspaceCommandUsageFn
	BackendWorkspacePermissionCommand          BackendWorkspacePermissionCommandFn

	// Action callbacks
	CompleteMenuCommand        CompleteMenuCommandFn
	ReplyCommandActionResponse ReplyCommandActionResponseFn
	CommandActionFromMessage   CommandActionFromMessageFn
	CommandMessageFromAction   CommandMessageFromActionFn

	// Formatting
	FormatMenuBody FormatMenuBodyFn

	// Render callbacks
	RenderNewCard                func(sessionKey, requestID string, payload NewPayload) map[string]any
	RenderCloneCard              func(sessionKey, requestID string, payload ClonePayload) map[string]any
	RenderClonePreparingCard     func(requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any
	RenderCloneSuccessCard       func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderSwitchExistingCard     func(sessionKey, workspaceID, targetDir, notice string) map[string]any
	RenderCloneSwitchExistingCard func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderCloneManualHintCard    func(sessionKey, workspaceID, targetDir, errText string) map[string]any
	RenderCloneCanceledCard      func(sessionKey string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any
	RenderMenuCard               RenderWorkspaceMenuCardFn
}

// ---------------------------------------------------------------------------
// RenderService
// ---------------------------------------------------------------------------

// RenderService handles all workspace card rendering.
type RenderService struct {
	App App

	// Session state
	GetSession GetSessionFn

	// Backend config
	BackendWorkspaceSummaryLines  BackendWorkspaceSummaryLinesFn
	BackendWorkspaceConfigButtons BackendWorkspaceConfigButtonsFn

	// Formatting
	FormatMenuBody FormatMenuBodyFn

	// Path picker render callback (from app/)
	RenderPathPickerCard func(requestID string, payload PathPickerPayload) (map[string]any, error)

	// Management helpers
	DefaultWorkspaceCloneRoot   func(ws *config.Workspace) string
	DefaultWorkspaceCloneParent func(ws *config.Workspace) string
}

// ---------------------------------------------------------------------------
// ThreadService
// ---------------------------------------------------------------------------

// ThreadService handles workspace thread management (binding, starting,
// resuming) for both Claude and Codex backends.
type ThreadService struct {
	App App

	// State callbacks
	GetSession  GetSessionFn
	SaveSession SaveSessionFn

	// Thread callbacks
	MarkSessionThreadLive MarkSessionThreadLiveFn

	// Session context callbacks
	SessionHasInFlight     SessionHasInFlightFn
	SwitchSessionWorkspace SwitchSessionWorkspaceFn
	ClearSessionThreadCtx  ClearSessionThreadCtxFn
	SetSessionThreadCtx    SetSessionThreadCtxFn
	SessionResetActiveOps  SessionResetActiveOpsFn

	// Codex client callbacks
	RequireCodexClient     RequireCodexClientFn
	BuildThreadStartParams BuildThreadStartParamsFn

	// Claude client callbacks
	RequireClaudeCore func() (appcore.ClaudeCore, error)
}
