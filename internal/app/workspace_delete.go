package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Delete-related methods on workspaceConfigService delegate to the
// workspacecmd ConfigService.

func (s workspaceConfigService) showWorkspaceDeleteMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspaceDeleteMenu(msg)
}

func (s workspaceConfigService) renderWorkspaceDeleteMenuCard(sessionKey string) (map[string]any, error) {
	return s.inner.RenderDeleteMenuCard(sessionKey)
}

func (s workspaceConfigService) renderWorkspaceDeleteConfirmCard(sessionKey, workspaceID string) (map[string]any, error) {
	return s.inner.RenderDeleteConfirmCard(sessionKey, workspaceID)
}

func (s workspaceConfigService) validateWorkspaceDeletion(sessionKey, workspaceID string) error {
	return s.inner.ValidateWorkspaceDeletion(sessionKey, workspaceID)
}

func (s workspaceConfigService) deleteWorkspace(sessionKey, workspaceID string) error {
	return s.inner.DeleteWorkspace(sessionKey, workspaceID)
}

// Delete-related action methods on workspaceService delegate to the
// workspacecmd ConfigService.

func (s workspaceService) completeWorkspaceDeleteMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return newWorkspaceConfigServiceInner(s.app).CompleteWorkspaceDeleteMenu(sessionKey)
}

func (s workspaceService) completeWorkspaceDeletePrompt(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return newWorkspaceConfigServiceInner(s.app).CompleteWorkspaceDeletePrompt(action, sessionKey, workspaceID)
}

func (s workspaceService) completeWorkspaceDeleteConfirm(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return newWorkspaceConfigServiceInner(s.app).CompleteWorkspaceDeleteConfirm(sessionKey, workspaceID)
}
