package app

import (
	"context"
	"fmt"
	"path/filepath"

	apppathpick "feidex/internal/app/pathpick"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type workspaceNewPayload = appworkspacecmd.NewPayload
type workspaceClonePayload = appworkspacecmd.ClonePayload
type workspaceCloneTakeoverError = appworkspacecmd.CloneTakeoverError
type workspaceCloneExistingDirError = appworkspacecmd.CloneExistingDirError
type workspaceCloneExistingWorkspaceError = appworkspacecmd.CloneExistingWorkspaceError
type workspaceCloneProgressSnapshot = appworkspacecmd.CloneProgressSnapshot
type workspaceClonePlan = appworkspacecmd.ClonePlan
type workspaceCloneProgressReporter = appworkspacecmd.CloneProgressReporter
type workspaceCloneOperation = appworkspacecmd.CloneOperation

type pathPickerPayload = appworkspacecmd.PathPickerPayload
type pathPickerEntry = apppathpick.Entry

const workspaceCommandUsage = appworkspacecmd.CommandUsage

const (
	pathPickerKind          = appworkspacecmd.PathPickerKind
	pathPickerModeDirectory = appworkspacecmd.PathPickerModeDirectory
	pathPickerModeFile      = appworkspacecmd.PathPickerModeFile
	pathPickerStyleDropdown = appworkspacecmd.PathPickerStyleDropdown
)

var parseWorkspaceCloneArgs = appworkspacecmd.ParseCloneArgs
var workspaceSandboxOptions = appworkspacecmd.SandboxOptions
var workspaceApprovalPolicyOptions = appworkspacecmd.ApprovalPolicyOptions
var workspaceNewPayloadFromPending = appworkspacecmd.NewPayloadFromPending
var workspaceClonePayloadFromPending = appworkspacecmd.ClonePayloadFromPending

func normalizePathPickerMode(mode string) string {
	return apppathpick.NormalizePathPickerMode(mode)
}

func normalizePathPickerStyle(style string) string {
	return apppathpick.NormalizePathPickerStyle(style)
}

func resolvePathPickerRoot(ws *config.Workspace) (string, error) {
	return apppathpick.ResolvePathPickerRoot(ws)
}

func resolvePathPickerPath(rootPath, candidate string) (string, error) {
	return apppathpick.ResolvePathPickerPath(rootPath, candidate)
}

func pathPickerWithinRoot(rootPath, candidate string) bool {
	return apppathpick.WithinRoot(rootPath, candidate)
}

func buildPathPickerDropdownElement(requestID string, payload pathPickerPayload, entries []pathPickerEntry) map[string]any {
	return apppathpick.BuildDropdownElement(requestID, payload, entries)
}

func buildPathPickerFooterElement(requestID string, payload pathPickerPayload) map[string]any {
	return apppathpick.BuildFooterElement(requestID, payload)
}

func listPathPickerEntries(payload pathPickerPayload) ([]pathPickerEntry, int, int, int, error) {
	return apppathpick.ListPathPickerEntries(payload)
}

func renderPathPickerEntryLabel(entry pathPickerEntry) string {
	return apppathpick.RenderEntryLabel(entry)
}

func encodePathPickerOption(entry pathPickerEntry) string {
	return apppathpick.EncodeOption(entry)
}

func decodePathPickerOption(raw string) (path string, isDir bool, ok bool) {
	return apppathpick.DecodeOption(raw)
}

type workspaceService struct {
	app *App
}

func newWorkspaceService(app *App) workspaceService {
	return workspaceService{app: app}
}

func (s workspaceService) mgmt() *appworkspacecmd.ManagementService {
	return newWorkspaceManagementServiceInner(s.app)
}

func (s workspaceService) cfg() *appworkspacecmd.ConfigService {
	return newWorkspaceConfigServiceInner(s.app)
}

func completeMenuWorkspace(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return newWorkspaceConfigServiceInner(a).CompleteMenuWorkspace(action, sessionKey)
}

func updateWorkspaceDefaults(a *App, workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	mutate(ws)
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return nil, err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return nil, err
	}
	return config.FindWorkspace(a.cfg, workspaceID), nil
}

func (s workspaceService) commandWorkspace(msg *feishu.InboundMessage, args []string) error {
	return commandWorkspace(s.app, msg, args)
}

func (s workspaceService) completeWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceUse(action, sessionKey, workspaceID)
}

func (s workspaceService) completeWorkspaceUseExisting(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceUseExisting(action, sessionKey, workspaceID)
}

func (s workspaceService) completeWorkspaceNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNew(action, sessionKey)
}

func (s workspaceService) completeWorkspaceDeleteMenu(_ *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.cfg().CompleteWorkspaceDeleteMenu(sessionKey)
}

func (s workspaceService) completeWorkspaceClone(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceClone(action, sessionKey)
}

func (s workspaceService) completeWorkspaceNewTakeover(action *feishu.CardAction, sessionKey, workspaceID, targetDir string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNewTakeover(action, sessionKey, workspaceID, targetDir)
}

func (s workspaceService) completeWorkspaceCloneUseExisting(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneUseExisting(action, sessionKey, workspaceID)
}

func (s workspaceService) completeWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
}

func (s workspaceService) completeWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
}

func (s workspaceService) completeWorkspaceSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceSandboxMenu(action, sessionKey)
}

func (s workspaceService) completeWorkspacePolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspacePolicyMenu(action, sessionKey)
}

func (s workspaceService) completeClaudeWorkspacePermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteClaudeWorkspacePermissionMenu(action, sessionKey)
}

func (s workspaceService) completePathPickerAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	return completePathPickerAction(s.app, action, actionName)
}

func (s workspaceService) completeWorkspaceNewPickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNewPickDir(action)
}

func (s workspaceService) completeWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNewSubmit(action)
}

func (s workspaceService) completeWorkspaceClonePickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceClonePickDir(action)
}

func (s workspaceService) completeWorkspaceCloneRefresh(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneRefresh(action)
}

func (s workspaceService) completeWorkspaceCloneSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneSubmit(action)
}

func (s workspaceService) completeWorkspaceCloneCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneCancel(action)
}

func (s pendingInputService) completeWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceNewText(msg, pending)
}

type workspaceManagementService struct {
	inner *appworkspacecmd.ManagementService
}

func newWorkspaceManagementService(app *App) workspaceManagementService {
	return workspaceManagementService{inner: newWorkspaceManagementServiceInner(app)}
}

func (s workspaceManagementService) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	return s.inner.BeginWorkspaceNew(msg)
}

func (s workspaceManagementService) beginWorkspaceNewWithPayload(msg *feishu.InboundMessage, sessionKey string, payload workspaceNewPayload) error {
	return s.inner.BeginWorkspaceNewWithPayload(msg, sessionKey, payload)
}

func (s workspaceManagementService) createWorkspaceNewPending(sessionKey, userID, feishuMsgID string, payload workspaceNewPayload) (string, error) {
	return s.inner.CreateWorkspaceNewPending(sessionKey, userID, feishuMsgID, payload)
}

func (s workspaceManagementService) workspaceByCWD(targetDir string) *config.Workspace {
	return s.inner.WorkspaceByCWD(targetDir)
}

func (s workspaceManagementService) workspaceByIDAndCWD(workspaceID, targetDir string) *config.Workspace {
	return s.inner.WorkspaceByIDAndCWD(workspaceID, targetDir)
}

func (s workspaceManagementService) createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	return s.inner.CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd)
}

func (s workspaceManagementService) setWorkspaceCloneOperation(requestID string, op *workspaceCloneOperation) {
	s.inner.SetWorkspaceCloneOperation(requestID, op)
}

func (s workspaceManagementService) workspaceCloneOperation(requestID string) *workspaceCloneOperation {
	return s.inner.GetWorkspaceCloneOperation(requestID)
}

func (s workspaceManagementService) clearWorkspaceCloneOperation(requestID string) {
	s.inner.ClearWorkspaceCloneOperation(requestID)
}

func (s workspaceManagementService) finishWorkspaceCloneSubmit(ctx context.Context, op *workspaceCloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir string, payload workspaceClonePayload) {
	s.inner.FinishWorkspaceCloneSubmit(ctx, op, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir, payload)
}

func (s workspaceManagementService) prepareWorkspaceClone(repoURL, explicitID, parentDir string) (*workspaceClonePlan, error) {
	return s.inner.PrepareWorkspaceClone(repoURL, explicitID, parentDir)
}

func (s workspaceManagementService) cloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report workspaceCloneProgressReporter) (string, string, error) {
	return s.inner.CloneWorkspaceInParent(ctx, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir, report)
}

func (s workspaceManagementService) cloneWorkspaceAndSwitch(msg *feishu.InboundMessage, repoURL, explicitID string) error {
	return s.inner.CloneWorkspaceAndSwitch(msg, repoURL, explicitID)
}

func (s workspaceManagementService) cloneWorkspaceAndSwitchInSelectedParent(msg *feishu.InboundMessage, repoURL, explicitID, parentDir string) error {
	return s.inner.CloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, explicitID, parentDir)
}

type workspaceConfigService struct {
	app   *App
	inner *appworkspacecmd.ConfigService
}

func newWorkspaceConfigService(app *App) workspaceConfigService {
	return workspaceConfigService{app: app, inner: newWorkspaceConfigServiceInner(app)}
}

func (s workspaceConfigService) showWorkspaceMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspaceMenu(msg)
}

func (s workspaceConfigService) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)
}

func (s workspaceConfigService) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	return s.inner.CurrentWorkspaceForMessage(msg)
}

func (s workspaceConfigService) currentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	return currentThreadForMessage(s.app, msg)
}

func (s workspaceConfigService) showWorkspaceSandboxMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspaceSandboxMenu(msg)
}

func (s workspaceConfigService) renderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceSandboxMenuCard(sessionKey)
}

func (s workspaceConfigService) showWorkspacePolicyMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspacePolicyMenu(msg)
}

func (s workspaceConfigService) renderWorkspacePolicyMenuCard(sessionKey string) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspacePolicyMenuCard(sessionKey)
}

func (s workspaceConfigService) showWorkspaceDeleteMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspaceDeleteMenu(msg)
}

func (s workspaceConfigService) renderWorkspaceDeleteMenuCard(sessionKey string) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceDeleteMenuCard(sessionKey)
}

func (s workspaceConfigService) renderWorkspaceDeleteConfirmCard(sessionKey, workspaceID string) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
}

func (s workspaceConfigService) validateWorkspaceDeletion(sessionKey, workspaceID string) error {
	return s.inner.ValidateWorkspaceDeletion(sessionKey, workspaceID)
}

func (s workspaceConfigService) deleteWorkspace(sessionKey, workspaceID string) error {
	return s.inner.DeleteWorkspace(sessionKey, workspaceID)
}

type workspaceRenderService struct {
	inner *appworkspacecmd.RenderService
}

func newWorkspaceRenderService(app *App) workspaceRenderService {
	return workspaceRenderService{inner: newWorkspaceRenderServiceInner(app)}
}

func (s workspaceRenderService) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	return s.inner.RenderWorkspaceMenuCard(sessionKey)
}

func (s workspaceRenderService) renderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return s.inner.RenderWorkspaceSandboxMenuCard(sessionKey)
}

func (s workspaceRenderService) renderWorkspacePolicyMenuCard(sessionKey string) (map[string]any, error) {
	return s.inner.RenderWorkspacePolicyMenuCard(sessionKey)
}

func (s workspaceRenderService) renderWorkspaceDeleteMenuCard(sessionKey string) (map[string]any, error) {
	return s.inner.RenderWorkspaceDeleteMenuCard(sessionKey)
}

func (s workspaceRenderService) renderWorkspaceDeleteConfirmCard(sessionKey, workspaceID string) (map[string]any, error) {
	return s.inner.RenderWorkspaceDeleteConfirmCard(sessionKey, workspaceID)
}

func (s workspaceRenderService) renderWorkspaceNewCard(sessionKey, requestID string, payload workspaceNewPayload) map[string]any {
	return s.inner.RenderWorkspaceNewCard(sessionKey, requestID, payload)
}

func (s workspaceRenderService) renderWorkspaceCloneCard(sessionKey, requestID string, payload workspaceClonePayload) map[string]any {
	return s.inner.RenderWorkspaceCloneCard(sessionKey, requestID, payload)
}

func (s workspaceRenderService) renderWorkspaceClonePreparingCard(requestID string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) map[string]any {
	return s.inner.RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
}

func (s workspaceRenderService) renderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	return s.inner.RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
}

func (s workspaceRenderService) renderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice string) map[string]any {
	return s.inner.RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
}

func (s workspaceRenderService) renderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir string) map[string]any {
	return s.inner.RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
}

func (s workspaceRenderService) renderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	return s.inner.RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
}

func (s workspaceRenderService) renderWorkspaceCloneCanceledCard(sessionKey string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) map[string]any {
	return s.inner.RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
}

func (s workspaceRenderService) renderPathPickerCard(requestID string, payload pathPickerPayload) (map[string]any, error) {
	return s.inner.RenderPathPickerCard(requestID, payload)
}

type workspaceThreadService struct {
	inner *appworkspacecmd.ThreadService
}

func newWorkspaceThreadService(app *App) workspaceThreadService {
	return workspaceThreadService{inner: newWorkspaceThreadServiceInner(app)}
}

func (s workspaceThreadService) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return s.inner.ListWorkspaceThreads(sessionKey, ws, includeAll)
}

func (s workspaceThreadService) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return s.inner.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (s workspaceThreadService) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return s.inner.StartWorkspaceThread(sessionKey, sess, ws)
}
