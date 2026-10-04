package feishuapp

import (
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
	"feidex/internal/state"

	workspacecards "feidex/internal/adapter/feishu/workspace"
)

type BackendRuntimeHandle = runtime.BackendHandle

// NewEffectRunner creates the frontend effect executor after the shell and its
// transport have been composed.
func NewEffectRunner(a *App) runtime.EffectRunner { return buildEffectRunner(a) }

// NewStateView creates the frontend-scoped state projection.
func NewStateView(a *App) *appstate.Store {
	if a == nil {
		return nil
	}
	view := appstate.NewScoped(a.store, a.FrontendID(), a.configView().configuredBackend())
	view.RevisionMutex = a.ConfigMu()
	return view
}

// NewWorkspacePresentation creates the detached Feishu workspace view.
func NewWorkspacePresentation(a *App) *workspacecards.Presentation {
	return buildWorkspaceRenderService(a)
}

// NewDispatcher creates the application input dispatcher for this frontend.
func NewDispatcher(a *App) application.Dispatcher { return newInputDispatcher(a) }

// CanonicalizeStoredSessionKeys performs the one-time state migration required
// when a frontend enters the runtime.
func CanonicalizeStoredSessionKeys(store *state.Store) error {
	return canonicalizeStoredSessionKeys(store)
}

func BackendKind(a *App) string { return a.configView().configuredBackend() }

func BuildBackendRuntimeHandle(deps BackendRuntimeDeps, target string) (*BackendRuntimeHandle, error) {
	return buildBackendRuntimeHandle(deps, target)
}

func InstallBackendRuntime(deps BackendRuntimeDeps, handle *BackendRuntimeHandle) {
	if handle != nil {
		installBackendRuntime(deps, handle)
	}
}

func AttachEffectRunner(a *App, runner runtime.EffectRunner) {
	if a == nil || a.runtimeOwner == nil {
		return
	}
	a.runtimeOwner.EffectRunner = &runner
	if notifying, ok := a.feishu.(*appfeishuwrap.NotifyingFeishuClient); ok {
		a.feishu = &appfeishuwrap.EffectClient{
			NotifyingFeishuClient: notifying,
			Frontend:              identity.FrontendID(a.frontendID),
			Runner:                runner,
		}
	}
}

func AttachStateView(a *App, view *appstate.Store) {
	if a != nil {
		a.stateView = view
	}
}

func AttachWorkspacePresentation(a *App, presentation *workspacecards.Presentation) {
	if a != nil {
		a.bindings.WorkspacePresentation = presentation
	}
}

func AttachDispatcher(a *App, dispatcher application.Dispatcher) {
	if a != nil && a.runtimeOwner != nil {
		a.runtimeOwner.Dispatcher = &dispatcher
	}
}

// InstallFeishuPolicies installs the Feishu-specific routing policies after
// composition has created the application/runtime graph. Event callback
// binding itself lives in internal/app's thin boundary.
func InstallFeishuPolicies(a *App) {
	if a == nil {
		return
	}
	configureGroupMessagePolicy(a)
	configureGroupPrimaryEvents(a)
}
