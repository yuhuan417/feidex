package app

import (
	workspacecards "feidex/internal/adapter/feishu/workspace"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/composition"
	"feidex/internal/domain/identity"
	"feidex/internal/state"
)

func newWorkspaceRenderService(a *App) *workspacecards.Presentation {
	if a != nil && a.composition != nil {
		a.composition.workspaceMu.Lock()
		defer a.composition.workspaceMu.Unlock()
		if a.composition.workspaceRender != nil {
			return a.composition.workspaceRender
		}
		service := buildWorkspaceRenderService(a)
		a.composition.workspaceRender = service
		return service
	}
	return buildWorkspaceRenderService(a)
}

func buildWorkspaceRenderService(a *App) *workspacecards.Presentation {
	deps := composition.WorkspacePresentationDependencies{}
	if a != nil {
		deps.Frontend = identity.FrontendID(a.FrontendID())
		deps.Config, deps.ConfigPath, deps.Mutex, deps.Scopes = a.cfg, a.cfgPath, a.ConfigMu(), a.State()
		deps.Backend = func() string { return configuredBackend(a) }
	}
	return composition.NewWorkspacePresentation(deps)
}

func pendingBindingMessagePreview(pending *state.AgentBindingPendingMessage) string {
	return workspaceapp.PendingPreview(pending)
}
