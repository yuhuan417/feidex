// Package workspacecmd provides workspace configuration, creation, deletion,
// and thread binding services extracted from the app god package.
package workspacecmd

import (
	"context"
	"encoding/json"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	frontendclients "feidex/internal/runtime"
	"feidex/internal/textutil"
	"sort"
	"strings"
	"sync"

	appbackend "feidex/internal/adapter/feishu/backend"
	interactionapp "feidex/internal/application/interaction"
	appworkspace "feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/formatutil"
	runtimeworkspace "feidex/internal/runtime/workspace"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Type aliases from the workspace sub-package.
type (
	SettingOption               = appworkspace.SettingOption
	NewPayload                  = appworkspace.NewPayload
	ClonePayload                = appworkspace.ClonePayload
	WorktreePayload             = appworkspace.WorktreePayload
	CloneTakeoverError          = appworkspace.CloneTakeoverError
	CloneExistingDirError       = appworkspace.CloneExistingDirError
	CloneExistingWorkspaceError = appworkspace.CloneExistingWorkspaceError
	CloneProgressSnapshot       = appworkspace.CloneProgressSnapshot
	ClonePlan                   = appworkspace.ClonePlan
	CloneWorktreePlan           = appworkspace.CloneWorktreePlan
	WorktreePlan                = appworkspace.WorktreePlan
	CloneProgressReporter       = runtimeworkspace.CloneProgressReporter
	CloneOperation              = runtimeworkspace.CloneOperation
	CloneTracker                = runtimeworkspace.CloneTracker
	ThreadBinding               = appworkspace.ThreadBinding
	PathPickerPayload           = appworkspace.PathPickerPayload
)

// Constants from the workspace sub-package.
const (
	CommandUsage            = appworkspace.CommandUsage
	CloneProgressKeepLines  = runtimeworkspace.CloneProgressKeepLines
	ClonePatchInterval      = runtimeworkspace.ClonePatchInterval
	PathPickerKind          = appworkspace.PathPickerKind
	PathPickerModeDirectory = appworkspace.PathPickerModeDirectory
	PathPickerModeFile      = appworkspace.PathPickerModeFile
	PathPickerStyleDropdown = appworkspace.PathPickerStyleDropdown
	CloneModeWorkspace      = appworkspace.CloneModeWorkspace
	CloneModeWorktree       = appworkspace.CloneModeWorktree
)

// Var aliases from the workspace sub-package.
var (
	SandboxOptions               = appworkspace.SandboxOptions
	ApprovalPolicyOptions        = appworkspace.ApprovalPolicyOptions
	MultiAgentModeOptions        = appworkspace.MultiAgentModeOptions
	ParseCloneArgs               = appworkspace.ParseCloneArgs
	ParseWorktreeArgs            = appworkspace.ParseWorktreeArgs
	NormalizeCloneMode           = appworkspace.NormalizeCloneMode
	CloneCreatesWorktree         = appworkspace.CloneCreatesWorktree
	NewTakeoverPayload           = appworkspace.NewTakeoverPayload
	NewTakeoverPayloadWithNotice = appworkspace.NewTakeoverPayloadWithNotice
	NewExistingWorkspaceNotice   = appworkspace.NewExistingWorkspaceNotice
	NewTakeoverNotice            = appworkspace.NewTakeoverNotice
	SessionReferencesWorkspace   = appworkspace.SessionReferencesWorkspace
	CloneRepoName                = appworkspace.CloneRepoName
	CloneDefaultID               = appworkspace.CloneDefaultID
	SuggestedWorktreeID          = appworkspace.SuggestedWorktreeID
	SuggestedWorktreeBranch      = appworkspace.SuggestedWorktreeBranch
	GitClone                     = runtimeworkspace.GitClone
	GitWorktreeAdd               = runtimeworkspace.GitWorktreeAdd
	NewCloneTracker              = runtimeworkspace.NewCloneTracker
	NewCloneOperation            = runtimeworkspace.NewCloneOperation
	ReadCloneOutput              = runtimeworkspace.ReadCloneOutput
)

func NewPayloadFromPending(pending *state.PendingRequest) NewPayload {
	var payload NewPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func ClonePayloadFromPending(pending *state.PendingRequest) ClonePayload {
	var payload ClonePayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func WorktreePayloadFromPending(pending *state.PendingRequest) WorktreePayload {
	var payload WorktreePayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func SortThreadsByUpdated(items []conversation.ThreadEntry) {
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
}

// ---------------------------------------------------------------------------
// Dependencies is the workspace command capability set. It is assembled at the
// composition root; workspace commands never receive the application root.
// ---------------------------------------------------------------------------

// Dependencies provides config, state, lifecycle, and Feishu capabilities.
type Dependencies struct {
	Settings       appworkspace.SettingsService
	Planning       *appworkspace.PlanningService
	Workflow       *appworkspace.Workflow
	Forms          *interactionapp.FormService
	ConfigProvider interface {
		Config() *config.Config
		ConfigMu() *sync.RWMutex
		Backend() string
		FrontendID() string
		FrontendConfigIndex() int
		Store() *state.Store
		WorkspaceSelection() appworkspace.SelectionService
		ConfigPath() string
	}
	Outbound         Outbound
	CardRenderer     CardRenderer
	BotNameFn        func() string
	ContextProvider  interface{ Context() context.Context }
	BackendDriver    appbackend.Driver
	SettingsRenderer interface {
		RenderWorkspaceSandboxMenuCard(string) (map[string]any, error)
		RenderWorkspacePolicyMenuCard(string) (map[string]any, error)
		RenderWorkspaceMultiAgentMenuCard(string) (map[string]any, error)
		RenderWorkspacePermissionModeMenuCard(string) (map[string]any, error)
	}
}

func (a Dependencies) Config() *config.Config {
	if a.ConfigProvider == nil {
		return nil
	}
	return a.ConfigProvider.Config()
}
func (a Dependencies) ConfigMu() *sync.RWMutex {
	if a.ConfigProvider == nil {
		return nil
	}
	return a.ConfigProvider.ConfigMu()
}
func (a Dependencies) Backend() string {
	if a.ConfigProvider == nil {
		return ""
	}
	return a.ConfigProvider.Backend()
}
func (a Dependencies) FrontendID() string {
	if a.ConfigProvider == nil {
		return ""
	}
	return a.ConfigProvider.FrontendID()
}
func (a Dependencies) FrontendConfigIndex() int {
	if a.ConfigProvider == nil {
		return -1
	}
	return a.ConfigProvider.FrontendConfigIndex()
}
func (a Dependencies) Store() *state.Store {
	if a.ConfigProvider == nil {
		return nil
	}
	return a.ConfigProvider.Store()
}
func (a Dependencies) ConfigPath() string {
	if a.ConfigProvider == nil {
		return ""
	}
	return a.ConfigProvider.ConfigPath()
}
func (a Dependencies) OutboundCapability() Outbound { return a.Outbound }
func (a Dependencies) Renderer() CardRenderer       { return a.CardRenderer }
func (a Dependencies) BotName() string {
	if a.BotNameFn == nil {
		return ""
	}
	return a.BotNameFn()
}
func (a Dependencies) Context() context.Context {
	if a.ContextProvider != nil {
		if c := a.ContextProvider.Context(); c != nil {
			return c
		}
	}
	return context.Background()
}

// Outbound is the semantic messaging capability used by workspace commands.
type Outbound interface {
	ReplyText(context.Context, string, string, bool) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	PatchCard(context.Context, string, map[string]any) error
}

type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}

func (a Dependencies) PermissionDriver() appbackend.PermissionDriver {
	if a.BackendDriver == nil {
		return nil
	}
	return a.BackendDriver.Permission()
}

// ---------------------------------------------------------------------------
// Callback function types
// ---------------------------------------------------------------------------

// State access callbacks.
type (
	GetSessionFn func(key string) *conversation.Session
	SessionsFn   func() []*conversation.Session
	PendingFn    func(id string) *state.PendingRequest
)

// Thread callbacks.
type (
	ListWorkspaceThreadsFn         func(sessionKey string, ws *config.Workspace, includeAll bool) ([]conversation.ThreadEntry, error)
	EnsureWorkspaceThreadBindingFn func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*ThreadBinding, error)
	StartWorkspaceThreadFn         func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*ThreadBinding, error)
	MarkSessionThreadLiveFn        func(sessionKey, threadID string)
	ClearSessionLiveThreadFn       func(sessionKey string)
)

// Session context callbacks.
type (
	SessionHasInFlightFn func(sess *conversation.Session) bool
)

// Clone operation callbacks.
type (
	SetCloneOpFn     func(requestID string, op *CloneOperation)
	GetCloneOpFn     func(requestID string) *CloneOperation
	ClearCloneOpFn   func(requestID string)
	GitCloneFn       func(ctx context.Context, repoURL, targetDir string, report CloneProgressReporter) error
	GitWorktreeAddFn func(ctx context.Context, baseRepoRoot, branchName, targetDir string) error
)

// Backend configuration callbacks.
type (
	BackendWorkspaceSwitchBindingNoticeFn        func(binding *ThreadBinding) string
	BackendWorkspaceSwitchBindingFailureNoticeFn func() string
	BackendWorkspaceSwitchInFlightNoticeFn       func() string
	BackendWorkspaceCommandUsageFn               func() string
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

// Async callbacks.
type (
	// RunAsyncFn runs a function asynchronously (e.g. in a goroutine).
	RunAsyncFn func(fn func())
	// OnAsyncDoneFn is called when async work completes. Tests use this
	// to synchronize with async goroutines.
	OnAsyncDoneFn func()
)

// Render callbacks.
type (
	RenderWorkspaceMenuCardFn           func(sessionKey string) map[string]any
	RenderWorkspaceSandboxMenuCardFn    func(sessionKey string) (map[string]any, error)
	RenderWorkspacePolicyMenuCardFn     func(sessionKey string) (map[string]any, error)
	RenderWorkspaceMultiAgentMenuCardFn func(sessionKey string) (map[string]any, error)
	RenderWorkspaceDeleteMenuCardFn     func(sessionKey string) (map[string]any, error)
	RenderWorkspaceDeleteConfirmCardFn  func(sessionKey, workspaceID string) (map[string]any, error)
)

// ---------------------------------------------------------------------------
// Grouped deps
// ---------------------------------------------------------------------------

type StateDeps struct {
	GetSession GetSessionFn
	Sessions   SessionsFn
	Pending    PendingFn
}

type SessionContextDeps struct {
	SessionHasInFlight     SessionHasInFlightFn
	ClearSessionLiveThread ClearSessionLiveThreadFn
}

type ThreadDeps struct {
	ListWorkspaceThreads         ListWorkspaceThreadsFn
	EnsureWorkspaceThreadBinding EnsureWorkspaceThreadBindingFn
	StartWorkspaceThread         StartWorkspaceThreadFn
	MarkSessionThreadLive        MarkSessionThreadLiveFn
	ClearSessionLiveThread       ClearSessionLiveThreadFn
}

type CloneDeps struct {
	SetCloneOp     SetCloneOpFn
	GetCloneOp     GetCloneOpFn
	ClearCloneOp   ClearCloneOpFn
	GitClone       GitCloneFn
	GitWorktreeAdd GitWorktreeAddFn
}

// BackendConfigDeps carries the backend-specific notices the workspace
// commands surface. Permission subcommands do not go through here: they are
// dispatched by ConfigService.CommandWorkspace through Dependencies.BackendDriver.
type BackendConfigDeps struct {
	BackendWorkspaceSwitchBindingNotice        BackendWorkspaceSwitchBindingNoticeFn
	BackendWorkspaceSwitchBindingFailureNotice BackendWorkspaceSwitchBindingFailureNoticeFn
	BackendWorkspaceSwitchInFlightNotice       BackendWorkspaceSwitchInFlightNoticeFn
	BackendWorkspaceCommandUsage               BackendWorkspaceCommandUsageFn
}

type ActionDeps struct {
	CompleteMenuCommand        CompleteMenuCommandFn
	ReplyCommandActionResponse ReplyCommandActionResponseFn
	CommandActionFromMessage   CommandActionFromMessageFn
	CommandMessageFromAction   CommandMessageFromActionFn
}

type FormattingDeps struct {
	FormatMenuBody FormatMenuBodyFn
}

type AsyncDeps struct {
	RunAsync    RunAsyncFn
	OnAsyncDone OnAsyncDoneFn
}

type ConfigRenderDeps struct {
	RenderMenuCard                RenderWorkspaceMenuCardFn
	RenderChooseMenuCard          RenderWorkspaceMenuCardFn
	RenderSandboxMenuCard         RenderWorkspaceSandboxMenuCardFn
	RenderPolicyMenuCard          RenderWorkspacePolicyMenuCardFn
	RenderMultiAgentMenuCard      RenderWorkspaceMultiAgentMenuCardFn
	RenderDeleteMenuCard          RenderWorkspaceDeleteMenuCardFn
	RenderDeleteConfirmCard       RenderWorkspaceDeleteConfirmCardFn
	RenderCloneSwitchExistingCard func(sessionKey, workspaceID, targetDir string) map[string]any
}

type ManagementRenderDeps struct {
	RenderNewCard                 func(sessionKey, requestID string, payload NewPayload) map[string]any
	RenderCloneCard               func(sessionKey, requestID string, payload ClonePayload) map[string]any
	RenderClonePreparingCard      func(requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any
	RenderCloneSuccessCard        func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderWorktreeCard            func(sessionKey, requestID string, payload WorktreePayload) map[string]any
	RenderWorktreePreparingCard   func(requestID string, payload WorktreePayload, plan *WorktreePlan, snapshot CloneProgressSnapshot) map[string]any
	RenderWorktreeSuccessCard     func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderWorktreeManualHintCard  func(sessionKey, workspaceID, targetDir, errText string) map[string]any
	RenderWorktreeCanceledCard    func(sessionKey string, payload WorktreePayload, plan *WorktreePlan, snapshot CloneProgressSnapshot) map[string]any
	RenderSwitchExistingCard      func(sessionKey, workspaceID, targetDir, notice string) map[string]any
	RenderCloneSwitchExistingCard func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderCloneManualHintCard     func(sessionKey, workspaceID, targetDir, errText string) map[string]any
	RenderCloneCanceledCard       func(sessionKey string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any
	RenderMenuCard                RenderWorkspaceMenuCardFn
}

type ClaudeDeps struct {
	RequireClaudeCore func() (frontendclients.ClaudeCore, error)
}

type ConfigDeps struct {
	Dependencies   Dependencies
	State          StateDeps
	SessionContext SessionContextDeps
	Threads        ThreadDeps
	Backend        BackendConfigDeps
	Actions        ActionDeps
	Formatting     FormattingDeps
	Render         ConfigRenderDeps
}

type ManagementDeps struct {
	Dependencies   Dependencies
	State          StateDeps
	SessionContext SessionContextDeps
	Threads        ThreadDeps
	Clone          CloneDeps
	Backend        BackendConfigDeps
	Actions        ActionDeps
	Formatting     FormattingDeps
	Async          AsyncDeps
	Render         ManagementRenderDeps
}

// ---------------------------------------------------------------------------
// ConfigService
// ---------------------------------------------------------------------------

// ConfigService handles workspace listing, configuration menus (sandbox,
// policy), and workspace deletion.
type ConfigService struct {
	Deps Dependencies
	deps ConfigDeps
}

// WorkspaceDeleteActions is the narrow, App-free owner for delete card
// callbacks. It carries only the workflow and presentation capabilities those
// actions need.
type WorkspaceDeleteActions struct {
	workflow                *appworkspace.Workflow
	renderDeleteMenuCard    func(string) (map[string]any, error)
	renderDeleteConfirmCard func(string, string) (map[string]any, error)
	renderWorkspaceMenuCard func(string) map[string]any
}

func (s ConfigService) WorkspaceDeleteActions() WorkspaceDeleteActions {
	return WorkspaceDeleteActions{
		workflow:                s.Deps.Workflow,
		renderDeleteMenuCard:    s.deps.Render.RenderDeleteMenuCard,
		renderDeleteConfirmCard: s.deps.Render.RenderDeleteConfirmCard,
		renderWorkspaceMenuCard: s.deps.Render.RenderMenuCard,
	}
}

func (s WorkspaceDeleteActions) CompleteWorkspaceDeletePrompt(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	workspaceID = firstNonEmpty(strings.TrimSpace(workspaceID), strings.TrimSpace(action.Option))
	if err := s.workflow.ValidateDeletion(sessionKey, workspaceID); err != nil {
		card, renderErr := s.renderDeleteMenuCard(sessionKey)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	card, err := s.renderDeleteConfirmCard(sessionKey, workspaceID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: "确认后只删除配置，不删除目录"},
		Card:  rawCard(card),
	}, nil
}

func (s WorkspaceDeleteActions) CompleteWorkspaceDeleteConfirm(sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	if err := s.workflow.Delete(sessionKey, workspaceID); err != nil {
		card, renderErr := s.renderDeleteMenuCard(sessionKey)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已删除工作区 " + strings.TrimSpace(workspaceID)},
		Card:  rawCard(s.renderWorkspaceMenuCard(sessionKey)),
	}, nil
}

// NewConfigService creates a new ConfigService.
func NewConfigService(deps ConfigDeps) *ConfigService {
	return &ConfigService{Deps: deps.Dependencies, deps: deps}
}

func (s ConfigService) GetSession(key string) *conversation.Session {
	if s.deps.State.GetSession == nil {
		return nil
	}
	return s.deps.State.GetSession(key)
}
func (s ConfigService) Sessions() []*conversation.Session {
	if s.deps.State.Sessions == nil {
		return nil
	}
	return s.deps.State.Sessions()
}
func (s ConfigService) Pending(id string) *state.PendingRequest {
	if s.deps.State.Pending == nil {
		return nil
	}
	return s.deps.State.Pending(id)
}
func (s ConfigService) SessionHasInFlight(sess *conversation.Session) bool {
	if s.deps.SessionContext.SessionHasInFlight == nil {
		return false
	}
	return s.deps.SessionContext.SessionHasInFlight(sess)
}
func (s ConfigService) ClearSessionLiveThread(sessionKey string) {
	clearFn := s.deps.Threads.ClearSessionLiveThread
	if clearFn == nil {
		clearFn = s.deps.SessionContext.ClearSessionLiveThread
	}
	if clearFn != nil {
		clearFn(sessionKey)
	}
}
func (s ConfigService) EnsureWorkspaceThreadBinding(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*ThreadBinding, error) {
	if s.deps.Threads.EnsureWorkspaceThreadBinding == nil {
		return nil, nil
	}
	return s.deps.Threads.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
}
func (s ConfigService) BackendWorkspaceSwitchBindingNotice(binding *ThreadBinding) string {
	if s.deps.Backend.BackendWorkspaceSwitchBindingNotice == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceSwitchBindingNotice(binding)
}
func (s ConfigService) BackendWorkspaceSwitchBindingFailureNotice() string {
	if s.deps.Backend.BackendWorkspaceSwitchBindingFailureNotice == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceSwitchBindingFailureNotice()
}
func (s ConfigService) BackendWorkspaceSwitchInFlightNotice() string {
	if s.deps.Backend.BackendWorkspaceSwitchInFlightNotice == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceSwitchInFlightNotice()
}
func (s ConfigService) BackendWorkspaceCommandUsage() string {
	if s.deps.Backend.BackendWorkspaceCommandUsage == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceCommandUsage()
}
func (s ConfigService) CompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	if s.deps.Actions.CompleteMenuCommand == nil {
		return nil, nil
	}
	return s.deps.Actions.CompleteMenuCommand(action, sessionKey, rawCommand, parentAction)
}
func (s ConfigService) ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	if s.deps.Actions.ReplyCommandActionResponse == nil {
		return nil
	}
	return s.deps.Actions.ReplyCommandActionResponse(msg, resp)
}
func (s ConfigService) CommandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if s.deps.Actions.CommandActionFromMessage == nil {
		return nil
	}
	return s.deps.Actions.CommandActionFromMessage(msg, actionValue)
}
func (s ConfigService) FormatMenuBody(action, body string) string {
	if s.deps.Formatting.FormatMenuBody == nil {
		return body
	}
	return s.deps.Formatting.FormatMenuBody(action, body)
}
func (s ConfigService) RenderMenuCard(sessionKey string) map[string]any {
	if s.deps.Render.RenderMenuCard == nil {
		return nil
	}
	return s.deps.Render.RenderMenuCard(sessionKey)
}
func (s ConfigService) RenderChooseMenuCard(sessionKey string) map[string]any {
	if s.deps.Render.RenderChooseMenuCard == nil {
		return nil
	}
	return s.deps.Render.RenderChooseMenuCard(sessionKey)
}
func (s ConfigService) RenderSandboxMenuCard(sessionKey string) (map[string]any, error) {
	if s.deps.Render.RenderSandboxMenuCard == nil {
		return nil, nil
	}
	return s.deps.Render.RenderSandboxMenuCard(sessionKey)
}
func (s ConfigService) RenderPolicyMenuCard(sessionKey string) (map[string]any, error) {
	if s.deps.Render.RenderPolicyMenuCard == nil {
		return nil, nil
	}
	return s.deps.Render.RenderPolicyMenuCard(sessionKey)
}
func (s ConfigService) RenderMultiAgentMenuCard(sessionKey string) (map[string]any, error) {
	if s.deps.Render.RenderMultiAgentMenuCard == nil {
		return nil, nil
	}
	return s.deps.Render.RenderMultiAgentMenuCard(sessionKey)
}
func (s ConfigService) RenderDeleteMenuCard(sessionKey string) (map[string]any, error) {
	if s.deps.Render.RenderDeleteMenuCard == nil {
		return nil, nil
	}
	return s.deps.Render.RenderDeleteMenuCard(sessionKey)
}
func (s ConfigService) RenderDeleteConfirmCard(sessionKey, workspaceID string) (map[string]any, error) {
	if s.deps.Render.RenderDeleteConfirmCard == nil {
		return nil, nil
	}
	return s.deps.Render.RenderDeleteConfirmCard(sessionKey, workspaceID)
}
func (s ConfigService) RenderCloneSwitchExistingCard(sessionKey, workspaceID, targetDir string) map[string]any {
	if s.deps.Render.RenderCloneSwitchExistingCard == nil {
		return nil
	}
	return s.deps.Render.RenderCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
}

// ---------------------------------------------------------------------------
// ManagementService
// ---------------------------------------------------------------------------

// ManagementService handles workspace creation, cloning, and workspace
// use/switch operations.
type ManagementService struct {
	Deps Dependencies
	deps ManagementDeps
}

// NewManagementService creates a new ManagementService.
func NewManagementService(deps ManagementDeps) *ManagementService {
	return &ManagementService{Deps: deps.Dependencies, deps: deps}
}

func (s ManagementService) GetSession(key string) *conversation.Session {
	if s.deps.State.GetSession == nil {
		return nil
	}
	return s.deps.State.GetSession(key)
}
func (s ManagementService) Sessions() []*conversation.Session {
	if s.deps.State.Sessions == nil {
		return nil
	}
	return s.deps.State.Sessions()
}
func (s ManagementService) Pending(id string) *state.PendingRequest {
	if s.deps.State.Pending == nil {
		return nil
	}
	return s.deps.State.Pending(id)
}
func (s ManagementService) SessionHasInFlight(sess *conversation.Session) bool {
	if s.deps.SessionContext.SessionHasInFlight == nil {
		return false
	}
	return s.deps.SessionContext.SessionHasInFlight(sess)
}
func (s ManagementService) EnsureWorkspaceThreadBinding(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*ThreadBinding, error) {
	if s.deps.Threads.EnsureWorkspaceThreadBinding == nil {
		return nil, nil
	}
	return s.deps.Threads.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
}
func (s ManagementService) MarkSessionThreadLive(sessionKey, threadID string) {
	if s.deps.Threads.MarkSessionThreadLive != nil {
		s.deps.Threads.MarkSessionThreadLive(sessionKey, threadID)
	}
}
func (s ManagementService) ClearSessionLiveThread(sessionKey string) {
	clearFn := s.deps.Threads.ClearSessionLiveThread
	if clearFn == nil {
		clearFn = s.deps.SessionContext.ClearSessionLiveThread
	}
	if clearFn != nil {
		clearFn(sessionKey)
	}
}
func (s ManagementService) StartWorkspaceThread(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*ThreadBinding, error) {
	if s.deps.Threads.StartWorkspaceThread == nil {
		return nil, nil
	}
	return s.deps.Threads.StartWorkspaceThread(sessionKey, sess, ws)
}
func (s ManagementService) SetCloneOp(requestID string, op *CloneOperation) {
	if s.deps.Clone.SetCloneOp != nil {
		s.deps.Clone.SetCloneOp(requestID, op)
	}
}
func (s ManagementService) GetCloneOp(requestID string) *CloneOperation {
	if s.deps.Clone.GetCloneOp == nil {
		return nil
	}
	return s.deps.Clone.GetCloneOp(requestID)
}
func (s ManagementService) ClearCloneOp(requestID string) {
	if s.deps.Clone.ClearCloneOp != nil {
		s.deps.Clone.ClearCloneOp(requestID)
	}
}
func (s ManagementService) GitClone(ctx context.Context, repoURL, targetDir string, report CloneProgressReporter) error {
	if s.deps.Clone.GitClone == nil {
		return nil
	}
	return s.deps.Clone.GitClone(ctx, repoURL, targetDir, report)
}
func (s ManagementService) GitWorktreeAdd(ctx context.Context, baseRepoRoot, branchName, targetDir string) error {
	if s.deps.Clone.GitWorktreeAdd != nil {
		return s.deps.Clone.GitWorktreeAdd(ctx, baseRepoRoot, branchName, targetDir)
	}
	return GitWorktreeAdd(ctx, baseRepoRoot, branchName, targetDir)
}
func (s ManagementService) BackendWorkspaceSwitchBindingNotice(binding *ThreadBinding) string {
	if s.deps.Backend.BackendWorkspaceSwitchBindingNotice == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceSwitchBindingNotice(binding)
}
func (s ManagementService) BackendWorkspaceSwitchBindingFailureNotice() string {
	if s.deps.Backend.BackendWorkspaceSwitchBindingFailureNotice == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceSwitchBindingFailureNotice()
}
func (s ManagementService) BackendWorkspaceSwitchInFlightNotice() string {
	if s.deps.Backend.BackendWorkspaceSwitchInFlightNotice == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceSwitchInFlightNotice()
}
func (s ManagementService) BackendWorkspaceCommandUsage() string {
	if s.deps.Backend.BackendWorkspaceCommandUsage == nil {
		return ""
	}
	return s.deps.Backend.BackendWorkspaceCommandUsage()
}
func (s ManagementService) CompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	if s.deps.Actions.CompleteMenuCommand == nil {
		return nil, nil
	}
	return s.deps.Actions.CompleteMenuCommand(action, sessionKey, rawCommand, parentAction)
}
func (s ManagementService) ReplyCommandActionResponse(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	if s.deps.Actions.ReplyCommandActionResponse == nil {
		return nil
	}
	return s.deps.Actions.ReplyCommandActionResponse(msg, resp)
}
func (s ManagementService) CommandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if s.deps.Actions.CommandActionFromMessage == nil {
		return nil
	}
	return s.deps.Actions.CommandActionFromMessage(msg, actionValue)
}
func (s ManagementService) CommandMessageFromAction(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
	if s.deps.Actions.CommandMessageFromAction == nil {
		return nil
	}
	return s.deps.Actions.CommandMessageFromAction(action, sessionKey, rawCommand)
}
func (s ManagementService) FormatMenuBody(action, body string) string {
	if s.deps.Formatting.FormatMenuBody == nil {
		return body
	}
	return s.deps.Formatting.FormatMenuBody(action, body)
}
func (s ManagementService) RunAsync(fn func()) {
	if s.deps.Async.RunAsync != nil {
		s.deps.Async.RunAsync(fn)
	}
}
func (s ManagementService) OnAsyncDone() {
	if s.deps.Async.OnAsyncDone != nil {
		s.deps.Async.OnAsyncDone()
	}
}
func (s ManagementService) RenderNewCard(sessionKey, requestID string, payload NewPayload) map[string]any {
	if s.deps.Render.RenderNewCard == nil {
		return nil
	}
	return s.deps.Render.RenderNewCard(sessionKey, requestID, payload)
}
func (s ManagementService) RenderCloneCard(sessionKey, requestID string, payload ClonePayload) map[string]any {
	if s.deps.Render.RenderCloneCard == nil {
		return nil
	}
	return s.deps.Render.RenderCloneCard(sessionKey, requestID, payload)
}
func (s ManagementService) RenderClonePreparingCard(requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any {
	if s.deps.Render.RenderClonePreparingCard == nil {
		return nil
	}
	return s.deps.Render.RenderClonePreparingCard(requestID, payload, parentDir, snapshot)
}
func (s ManagementService) RenderCloneSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	if s.deps.Render.RenderCloneSuccessCard == nil {
		return nil
	}
	return s.deps.Render.RenderCloneSuccessCard(sessionKey, workspaceID, targetDir)
}
func (s ManagementService) RenderWorktreeCard(sessionKey, requestID string, payload WorktreePayload) map[string]any {
	if s.deps.Render.RenderWorktreeCard == nil {
		return nil
	}
	return s.deps.Render.RenderWorktreeCard(sessionKey, requestID, payload)
}
func (s ManagementService) RenderWorktreePreparingCard(requestID string, payload WorktreePayload, plan *WorktreePlan, snapshot CloneProgressSnapshot) map[string]any {
	if s.deps.Render.RenderWorktreePreparingCard == nil {
		return nil
	}
	return s.deps.Render.RenderWorktreePreparingCard(requestID, payload, plan, snapshot)
}
func (s ManagementService) RenderWorktreeSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	if s.deps.Render.RenderWorktreeSuccessCard == nil {
		return nil
	}
	return s.deps.Render.RenderWorktreeSuccessCard(sessionKey, workspaceID, targetDir)
}
func (s ManagementService) RenderWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	if s.deps.Render.RenderWorktreeManualHintCard == nil {
		return nil
	}
	return s.deps.Render.RenderWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText)
}
func (s ManagementService) RenderWorktreeCanceledCard(sessionKey string, payload WorktreePayload, plan *WorktreePlan, snapshot CloneProgressSnapshot) map[string]any {
	if s.deps.Render.RenderWorktreeCanceledCard == nil {
		return nil
	}
	return s.deps.Render.RenderWorktreeCanceledCard(sessionKey, payload, plan, snapshot)
}
func (s ManagementService) RenderSwitchExistingCard(sessionKey, workspaceID, targetDir, notice string) map[string]any {
	if s.deps.Render.RenderSwitchExistingCard == nil {
		return nil
	}
	return s.deps.Render.RenderSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
}
func (s ManagementService) RenderCloneSwitchExistingCard(sessionKey, workspaceID, targetDir string) map[string]any {
	if s.deps.Render.RenderCloneSwitchExistingCard == nil {
		return nil
	}
	return s.deps.Render.RenderCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
}
func (s ManagementService) RenderCloneManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	if s.deps.Render.RenderCloneManualHintCard == nil {
		return nil
	}
	return s.deps.Render.RenderCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
}
func (s ManagementService) RenderCloneCanceledCard(sessionKey string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any {
	if s.deps.Render.RenderCloneCanceledCard == nil {
		return nil
	}
	return s.deps.Render.RenderCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
}
func (s ManagementService) RenderMenuCard(sessionKey string) map[string]any {
	if s.deps.Render.RenderMenuCard == nil {
		return nil
	}
	return s.deps.Render.RenderMenuCard(sessionKey)
}

func (a Dependencies) WorkspaceSelection() appworkspace.SelectionService {
	if a.ConfigProvider == nil {
		return appworkspace.SelectionService{}
	}
	return a.ConfigProvider.WorkspaceSelection()
}

func firstNonEmpty(values ...string) string {
	return textutil.FirstNonEmpty(values...)
}
func makeSessionKey(a Dependencies, msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.SessionKey) != "" {
		return identity.CanonicalSessionKey(a.FrontendID(), msg.SessionKey)
	}
	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return ""
	}
	frontendID := strings.TrimSpace(a.FrontendID())
	if frontendID == "" {
		return "feishu:chat:" + chatID
	}
	return "feishu:frontend:" + frontendID + ":chat:" + chatID
}

// Form values are decoded at the Feishu entrypoint before application policy
// sees them. Preserve the existing explicit-empty and non-string coercion rules.
func workspaceFormValues(values map[string]any) map[string]string {
	result := make(map[string]string, len(values))
	for key := range values {
		if value, ok := formatutil.FormValueString(values, key); ok {
			result[key] = value
		}
	}
	return result
}
func MergeNewFormValues(payload NewPayload, values map[string]any) NewPayload {
	return appworkspace.MergeNewFormValues(payload, workspaceFormValues(values))
}
func MergeCloneFormValues(payload ClonePayload, values map[string]any) ClonePayload {
	return appworkspace.MergeCloneFormValues(payload, workspaceFormValues(values))
}
func MergeWorktreeFormValues(payload WorktreePayload, values map[string]any) WorktreePayload {
	return appworkspace.MergeWorktreeFormValues(payload, workspaceFormValues(values))
}
