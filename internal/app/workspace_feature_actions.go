package app

import (
	"fmt"
	"path/filepath"

	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// workspaceService wraps appworkspacecmd.ManagementService action methods
// for backward compatibility with the action registry.
func (s workspaceService) mgmt() *appworkspacecmd.ManagementService {
	return newWorkspaceManagementServiceInner(s.app)
}

func completeMenuWorkspace(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, "/workspace", "menu.root")
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

func (s workspaceService) completeWorkspaceClone(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceClone(action, sessionKey)
}

func (s workspaceService) completeWorkspaceNewTakeover(action *feishu.CardAction, sessionKey, workspaceID, targetDir string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNewTakeover(action, sessionKey, workspaceID, targetDir)
}

func (s workspaceService) completeWorkspaceCloneUseExisting(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneUseExisting(action, sessionKey, workspaceID)
}

func (s workspaceService) completeWorkspaceClonePickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceClonePickDir(action)
}

func (s workspaceService) completeWorkspaceCloneCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneCancel(action)
}

func (s workspaceService) completeWorkspaceNewPickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNewPickDir(action)
}

func (s workspaceService) completeWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceNewSubmit(action)
}

func (s workspaceService) completeWorkspaceCloneSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceCloneSubmit(action)
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

func (s workspaceService) completeWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
}

func (s workspaceService) completeWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	return s.mgmt().CompleteWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
}
