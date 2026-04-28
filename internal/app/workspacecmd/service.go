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
	RenderWorkspaceMenuCardFn          func(sessionKey string) map[string]any
	RenderWorkspaceSandboxMenuCardFn   func(sessionKey string) (map[string]any, error)
	RenderWorkspacePolicyMenuCardFn    func(sessionKey string) (map[string]any, error)
	RenderWorkspaceDeleteMenuCardFn    func(sessionKey string) (map[string]any, error)
	RenderWorkspaceDeleteConfirmCardFn func(sessionKey, workspaceID string) (map[string]any, error)
)

// ---------------------------------------------------------------------------
// Grouped deps
// ---------------------------------------------------------------------------

type StateDeps struct {
	GetSession    GetSessionFn
	Sessions      SessionsFn
	SaveSession   SaveSessionFn
	NextLocalID   NextLocalIDFn
	Pending       PendingFn
	SavePending   SavePendingFn
	UpdatePending UpdatePendingFn
}

type SessionContextDeps struct {
	SessionHasInFlight     SessionHasInFlightFn
	SwitchSessionWorkspace SwitchSessionWorkspaceFn
	ClearSessionThreadCtx  ClearSessionThreadCtxFn
	SetSessionThreadCtx    SetSessionThreadCtxFn
	SessionResetActiveOps  SessionResetActiveOpsFn
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
	SetCloneOp   SetCloneOpFn
	GetCloneOp   GetCloneOpFn
	ClearCloneOp ClearCloneOpFn
	GitClone     GitCloneFn
}

type CodexDeps struct {
	RequireCodexClient     RequireCodexClientFn
	BuildThreadStartParams BuildThreadStartParamsFn
}

type BackendConfigDeps struct {
	BackendWorkspaceSummaryLines               BackendWorkspaceSummaryLinesFn
	BackendWorkspaceConfigButtons              BackendWorkspaceConfigButtonsFn
	BackendWorkspaceSwitchBindingNotice        BackendWorkspaceSwitchBindingNoticeFn
	BackendWorkspaceSwitchBindingFailureNotice BackendWorkspaceSwitchBindingFailureNoticeFn
	BackendWorkspaceSwitchInFlightNotice       BackendWorkspaceSwitchInFlightNoticeFn
	BackendWorkspaceCommandUsage               BackendWorkspaceCommandUsageFn
	BackendWorkspacePermissionCommand          BackendWorkspacePermissionCommandFn
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
	RenderDeleteMenuCard          RenderWorkspaceDeleteMenuCardFn
	RenderDeleteConfirmCard       RenderWorkspaceDeleteConfirmCardFn
	RenderCloneSwitchExistingCard func(sessionKey, workspaceID, targetDir string) map[string]any
}

type ManagementRenderDeps struct {
	RenderNewCard                 func(sessionKey, requestID string, payload NewPayload) map[string]any
	RenderCloneCard               func(sessionKey, requestID string, payload ClonePayload) map[string]any
	RenderClonePreparingCard      func(requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any
	RenderCloneSuccessCard        func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderSwitchExistingCard      func(sessionKey, workspaceID, targetDir, notice string) map[string]any
	RenderCloneSwitchExistingCard func(sessionKey, workspaceID, targetDir string) map[string]any
	RenderCloneManualHintCard     func(sessionKey, workspaceID, targetDir, errText string) map[string]any
	RenderCloneCanceledCard       func(sessionKey string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) map[string]any
	RenderMenuCard                RenderWorkspaceMenuCardFn
}

type PathPickerDeps struct {
	RenderPathPickerCard func(requestID string, payload PathPickerPayload) (map[string]any, error)
}

type RenderManagementDeps struct {
	DefaultWorkspaceCloneRoot   func(ws *config.Workspace) string
	DefaultWorkspaceCloneParent func(ws *config.Workspace) string
}

type ClaudeDeps struct {
	RequireClaudeCore func() (appcore.ClaudeCore, error)
}

type ConfigDeps struct {
	App            App
	State          StateDeps
	SessionContext SessionContextDeps
	Threads        ThreadDeps
	Backend        BackendConfigDeps
	Actions        ActionDeps
	Formatting     FormattingDeps
	Render         ConfigRenderDeps
}

type ManagementDeps struct {
	App            App
	State          StateDeps
	SessionContext SessionContextDeps
	Threads        ThreadDeps
	Clone          CloneDeps
	Codex          CodexDeps
	Backend        BackendConfigDeps
	Actions        ActionDeps
	Formatting     FormattingDeps
	Async          AsyncDeps
	Render         ManagementRenderDeps
}

type RenderDeps struct {
	App        App
	State      StateDeps
	Backend    BackendConfigDeps
	Formatting FormattingDeps
	PathPicker PathPickerDeps
	Management RenderManagementDeps
}

type ThreadServiceDeps struct {
	App            App
	State          StateDeps
	Threads        ThreadDeps
	SessionContext SessionContextDeps
	Codex          CodexDeps
	Claude         ClaudeDeps
}

// ---------------------------------------------------------------------------
// ConfigService
// ---------------------------------------------------------------------------

// ConfigService handles workspace listing, configuration menus (sandbox,
// policy), and workspace deletion.
type ConfigService struct {
	App  App
	deps ConfigDeps
}

// NewConfigService creates a new ConfigService.
func NewConfigService(deps ConfigDeps) *ConfigService {
	return &ConfigService{App: deps.App, deps: deps}
}

func (s ConfigService) GetSession(key string) *state.Session {
	if s.deps.State.GetSession == nil {
		return nil
	}
	return s.deps.State.GetSession(key)
}
func (s ConfigService) Sessions() []*state.Session {
	if s.deps.State.Sessions == nil {
		return nil
	}
	return s.deps.State.Sessions()
}
func (s ConfigService) SaveSession(sess *state.Session) error {
	if s.deps.State.SaveSession == nil {
		return nil
	}
	return s.deps.State.SaveSession(sess)
}
func (s ConfigService) NextLocalID(prefix string) (string, error) {
	if s.deps.State.NextLocalID == nil {
		return "", nil
	}
	return s.deps.State.NextLocalID(prefix)
}
func (s ConfigService) Pending(id string) *state.PendingRequest {
	if s.deps.State.Pending == nil {
		return nil
	}
	return s.deps.State.Pending(id)
}
func (s ConfigService) SavePending(req *state.PendingRequest) error {
	if s.deps.State.SavePending == nil {
		return nil
	}
	return s.deps.State.SavePending(req)
}
func (s ConfigService) UpdatePending(id string, mutate func(*state.PendingRequest)) error {
	if s.deps.State.UpdatePending == nil {
		return nil
	}
	return s.deps.State.UpdatePending(id, mutate)
}
func (s ConfigService) SessionHasInFlight(sess *state.Session) bool {
	if s.deps.SessionContext.SessionHasInFlight == nil {
		return false
	}
	return s.deps.SessionContext.SessionHasInFlight(sess)
}
func (s ConfigService) SwitchSessionWorkspace(sess *state.Session, workspaceID string) {
	if s.deps.SessionContext.SwitchSessionWorkspace != nil {
		s.deps.SessionContext.SwitchSessionWorkspace(sess, workspaceID)
	}
}
func (s ConfigService) ClearSessionThreadCtx(sess *state.Session) {
	if s.deps.SessionContext.ClearSessionThreadCtx != nil {
		s.deps.SessionContext.ClearSessionThreadCtx(sess)
	}
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
func (s ConfigService) EnsureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	if s.deps.Threads.EnsureWorkspaceThreadBinding == nil {
		return nil, nil
	}
	return s.deps.Threads.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
}
func (s ConfigService) BackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	if s.deps.Backend.BackendWorkspaceSummaryLines == nil {
		return lines
	}
	return s.deps.Backend.BackendWorkspaceSummaryLines(lines, currentWS)
}
func (s ConfigService) BackendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	if s.deps.Backend.BackendWorkspaceConfigButtons == nil {
		return nil
	}
	return s.deps.Backend.BackendWorkspaceConfigButtons(sessionKey)
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
func (s ConfigService) BackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	if s.deps.Backend.BackendWorkspacePermissionCommand == nil {
		return nil
	}
	return s.deps.Backend.BackendWorkspacePermissionCommand(msg, args, sessionKey)
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
	App  App
	deps ManagementDeps
}

// NewManagementService creates a new ManagementService.
func NewManagementService(deps ManagementDeps) *ManagementService {
	return &ManagementService{App: deps.App, deps: deps}
}

func (s ManagementService) GetSession(key string) *state.Session {
	if s.deps.State.GetSession == nil {
		return nil
	}
	return s.deps.State.GetSession(key)
}
func (s ManagementService) Sessions() []*state.Session {
	if s.deps.State.Sessions == nil {
		return nil
	}
	return s.deps.State.Sessions()
}
func (s ManagementService) SaveSession(sess *state.Session) error {
	if s.deps.State.SaveSession == nil {
		return nil
	}
	return s.deps.State.SaveSession(sess)
}
func (s ManagementService) NextLocalID(prefix string) (string, error) {
	if s.deps.State.NextLocalID == nil {
		return "", nil
	}
	return s.deps.State.NextLocalID(prefix)
}
func (s ManagementService) Pending(id string) *state.PendingRequest {
	if s.deps.State.Pending == nil {
		return nil
	}
	return s.deps.State.Pending(id)
}
func (s ManagementService) SavePending(req *state.PendingRequest) error {
	if s.deps.State.SavePending == nil {
		return nil
	}
	return s.deps.State.SavePending(req)
}
func (s ManagementService) UpdatePending(id string, mutate func(*state.PendingRequest)) error {
	if s.deps.State.UpdatePending == nil {
		return nil
	}
	return s.deps.State.UpdatePending(id, mutate)
}
func (s ManagementService) SessionHasInFlight(sess *state.Session) bool {
	if s.deps.SessionContext.SessionHasInFlight == nil {
		return false
	}
	return s.deps.SessionContext.SessionHasInFlight(sess)
}
func (s ManagementService) SwitchSessionWorkspace(sess *state.Session, workspaceID string) {
	if s.deps.SessionContext.SwitchSessionWorkspace != nil {
		s.deps.SessionContext.SwitchSessionWorkspace(sess, workspaceID)
	}
}
func (s ManagementService) ClearSessionThreadCtx(sess *state.Session) {
	if s.deps.SessionContext.ClearSessionThreadCtx != nil {
		s.deps.SessionContext.ClearSessionThreadCtx(sess)
	}
}
func (s ManagementService) SetSessionThreadCtx(sess *state.Session, workspaceID, threadID, name, preview string) {
	if s.deps.SessionContext.SetSessionThreadCtx != nil {
		s.deps.SessionContext.SetSessionThreadCtx(sess, workspaceID, threadID, name, preview)
	}
}
func (s ManagementService) SessionResetActiveOps(sess *state.Session) {
	if s.deps.SessionContext.SessionResetActiveOps != nil {
		s.deps.SessionContext.SessionResetActiveOps(sess)
	}
}
func (s ManagementService) EnsureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
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
func (s ManagementService) StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
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
func (s ManagementService) RequireCodexClient() (CodexClient, error) {
	if s.deps.Codex.RequireCodexClient == nil {
		return nil, nil
	}
	return s.deps.Codex.RequireCodexClient()
}
func (s ManagementService) BuildThreadStartParams(ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any {
	if s.deps.Codex.BuildThreadStartParams == nil {
		return nil
	}
	return s.deps.Codex.BuildThreadStartParams(ws, sess, effectiveModel)
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
func (s ManagementService) BackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	if s.deps.Backend.BackendWorkspacePermissionCommand == nil {
		return nil
	}
	return s.deps.Backend.BackendWorkspacePermissionCommand(msg, args, sessionKey)
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

// ---------------------------------------------------------------------------
// RenderService
// ---------------------------------------------------------------------------

// RenderService handles all workspace card rendering.
type RenderService struct {
	App  App
	deps RenderDeps
}

// NewRenderService creates a new RenderService.
func NewRenderService(deps RenderDeps) *RenderService {
	return &RenderService{App: deps.App, deps: deps}
}

func (s RenderService) GetSession(key string) *state.Session {
	if s.deps.State.GetSession == nil {
		return nil
	}
	return s.deps.State.GetSession(key)
}
func (s RenderService) BackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	if s.deps.Backend.BackendWorkspaceSummaryLines == nil {
		return lines
	}
	return s.deps.Backend.BackendWorkspaceSummaryLines(lines, currentWS)
}
func (s RenderService) BackendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	if s.deps.Backend.BackendWorkspaceConfigButtons == nil {
		return nil
	}
	return s.deps.Backend.BackendWorkspaceConfigButtons(sessionKey)
}
func (s RenderService) FormatMenuBody(action, body string) string {
	if s.deps.Formatting.FormatMenuBody == nil {
		return body
	}
	return s.deps.Formatting.FormatMenuBody(action, body)
}
func (s RenderService) RenderPathPickerCard(requestID string, payload PathPickerPayload) (map[string]any, error) {
	if s.deps.PathPicker.RenderPathPickerCard == nil {
		return nil, nil
	}
	return s.deps.PathPicker.RenderPathPickerCard(requestID, payload)
}
func (s RenderService) DefaultWorkspaceCloneRoot(ws *config.Workspace) string {
	if s.deps.Management.DefaultWorkspaceCloneRoot == nil {
		return ""
	}
	return s.deps.Management.DefaultWorkspaceCloneRoot(ws)
}
func (s RenderService) DefaultWorkspaceCloneParent(ws *config.Workspace) string {
	if s.deps.Management.DefaultWorkspaceCloneParent == nil {
		return ""
	}
	return s.deps.Management.DefaultWorkspaceCloneParent(ws)
}

// ---------------------------------------------------------------------------
// ThreadService
// ---------------------------------------------------------------------------

// ThreadService handles workspace thread management (binding, starting,
// resuming) for both Claude and Codex backends.
type ThreadService struct {
	App  App
	deps ThreadServiceDeps
}

// NewThreadService creates a new ThreadService.
func NewThreadService(deps ThreadServiceDeps) *ThreadService {
	return &ThreadService{App: deps.App, deps: deps}
}

func (s ThreadService) GetSession(key string) *state.Session {
	if s.deps.State.GetSession == nil {
		return nil
	}
	return s.deps.State.GetSession(key)
}
func (s ThreadService) SaveSession(sess *state.Session) error {
	if s.deps.State.SaveSession == nil {
		return nil
	}
	return s.deps.State.SaveSession(sess)
}
func (s ThreadService) MarkSessionThreadLive(sessionKey, threadID string) {
	if s.deps.Threads.MarkSessionThreadLive != nil {
		s.deps.Threads.MarkSessionThreadLive(sessionKey, threadID)
	}
}
func (s ThreadService) SessionHasInFlight(sess *state.Session) bool {
	if s.deps.SessionContext.SessionHasInFlight == nil {
		return false
	}
	return s.deps.SessionContext.SessionHasInFlight(sess)
}
func (s ThreadService) SwitchSessionWorkspace(sess *state.Session, workspaceID string) {
	if s.deps.SessionContext.SwitchSessionWorkspace != nil {
		s.deps.SessionContext.SwitchSessionWorkspace(sess, workspaceID)
	}
}
func (s ThreadService) ClearSessionThreadCtx(sess *state.Session) {
	if s.deps.SessionContext.ClearSessionThreadCtx != nil {
		s.deps.SessionContext.ClearSessionThreadCtx(sess)
	}
}
func (s ThreadService) SetSessionThreadCtx(sess *state.Session, workspaceID, threadID, name, preview string) {
	if s.deps.SessionContext.SetSessionThreadCtx != nil {
		s.deps.SessionContext.SetSessionThreadCtx(sess, workspaceID, threadID, name, preview)
	}
}
func (s ThreadService) SessionResetActiveOps(sess *state.Session) {
	if s.deps.SessionContext.SessionResetActiveOps != nil {
		s.deps.SessionContext.SessionResetActiveOps(sess)
	}
}
func (s ThreadService) RequireCodexClient() (CodexClient, error) {
	if s.deps.Codex.RequireCodexClient == nil {
		return nil, nil
	}
	return s.deps.Codex.RequireCodexClient()
}
func (s ThreadService) BuildThreadStartParams(ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any {
	if s.deps.Codex.BuildThreadStartParams == nil {
		return nil
	}
	return s.deps.Codex.BuildThreadStartParams(ws, sess, effectiveModel)
}
func (s ThreadService) RequireClaudeCore() (appcore.ClaudeCore, error) {
	if s.deps.Claude.RequireClaudeCore == nil {
		return nil, nil
	}
	return s.deps.Claude.RequireClaudeCore()
}
