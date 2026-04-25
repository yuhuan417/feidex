package app

import (
	appworkspacecmd "feidex/internal/app/workspacecmd"
)

// workspaceRenderService wraps appworkspacecmd.RenderService and provides
// lowercase method names for backward compatibility.
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
