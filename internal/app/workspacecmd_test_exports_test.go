package app

import (
	"fmt"
	"path/filepath"

	apppathpick "feidex/internal/app/pathpick"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	pathPickerKind          = appworkspacecmd.PathPickerKind
	pathPickerModeDirectory = appworkspacecmd.PathPickerModeDirectory
	pathPickerModeFile      = appworkspacecmd.PathPickerModeFile
	pathPickerStyleDropdown = appworkspacecmd.PathPickerStyleDropdown
)

func encodePathPickerOption(entry apppathpick.Entry) string {
	return apppathpick.EncodeOption(entry)
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

func (s workspaceService) completeWorkspaceNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNew(action, sessionKey)
}

func (s workspaceService) completeWorkspaceDeleteMenu(_ *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.cfg().CompleteWorkspaceDeleteMenu(sessionKey)
}

func (s workspaceService) completeWorkspaceClone(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceClone(action, sessionKey)
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

type workspaceConfigService struct {
	app   *App
	inner *appworkspacecmd.ConfigService
}

func newWorkspaceConfigService(app *App) workspaceConfigService {
	return workspaceConfigService{app: app, inner: newWorkspaceConfigServiceInner(app)}
}

func (s workspaceConfigService) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)
}

func (s workspaceConfigService) currentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	return currentThreadForMessage(s.app, msg)
}

func (s workspaceConfigService) renderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceSandboxMenuCard(sessionKey)
}

func (s workspaceConfigService) renderWorkspaceDeleteMenuCard(sessionKey string) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(s.app).RenderWorkspaceDeleteMenuCard(sessionKey)
}

type workspaceRenderService struct {
	inner *appworkspacecmd.RenderService
}

func newWorkspaceRenderService(app *App) workspaceRenderService {
	return workspaceRenderService{inner: newWorkspaceRenderServiceInner(app)}
}

func (s workspaceRenderService) renderWorkspaceNewCard(sessionKey, requestID string, payload appworkspacecmd.NewPayload) map[string]any {
	return s.inner.RenderWorkspaceNewCard(sessionKey, requestID, payload)
}

func (s workspaceRenderService) renderWorkspaceCloneCard(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
	return s.inner.RenderWorkspaceCloneCard(sessionKey, requestID, payload)
}

type workspaceThreadService struct {
	inner *appworkspacecmd.ThreadService
}

func newWorkspaceThreadService(app *App) workspaceThreadService {
	return workspaceThreadService{inner: newWorkspaceThreadServiceInner(app)}
}

func (s workspaceThreadService) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
	return s.inner.StartWorkspaceThread(sessionKey, sess, ws)
}
