package feishuapp

import (
	workspacecards "feidex/internal/adapter/feishu/workspace"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/compositionkit"
	"feidex/internal/domain/identity"
	"feidex/internal/state"
)

func buildWorkspaceRenderService(a *App) *workspacecards.Presentation {
	deps := compositionkit.WorkspacePresentationDependencies{}
	if a != nil {
		deps.Frontend = identity.FrontendID(a.FrontendID())
		deps.Config, deps.ConfigPath, deps.Mutex, deps.Scopes = a.cfg, a.cfgPath, a.ConfigMu(), a.State()
		deps.Backend = func() string { return configuredBackend(a) }
	}
	return compositionkit.NewWorkspacePresentation(deps)
}

func pendingBindingMessagePreview(pending *state.AgentBindingPendingMessage) string {
	return workspaceapp.PendingPreview(pending)
}
