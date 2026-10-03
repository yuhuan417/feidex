package app

import (
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/adapter/feishu/finalcardpatch"
	"feidex/internal/adapter/feishu/turnitem"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
	skillruntime "feidex/internal/runtime/skill"
	"feidex/internal/runtime/turnbinding"

	workspacecards "feidex/internal/adapter/feishu/workspace"
)

// Trackers and BackendRuntimeHandle are opaque runtime parts. Their concrete
// fields stay private to the Feishu adapter; composition only coordinates
// their construction and attachment to one frontend.
type Trackers = appTrackers
type BackendRuntimeHandle = backendRuntimeHandle

// NewTrackers creates the frontend-scoped Feishu tracker set. It is a boundary
// factory: the production construction sequence lives in internal/composition.
func NewTrackers(a *App) *Trackers {
	if a == nil || a.runtimeOwner == nil {
		return nil
	}
	return &appTrackers{
		turnStreams:        newTurnStreamTracker(),
		turnItems:          turnitem.NewTracker(),
		workspaceCloneOps:  newWorkspaceCloneTracker(),
		turnBindings:       turnbinding.NewTracker(a.store),
		finalCardPatches:   finalcardpatch.NewTracker(),
		pendingSkills:      skillruntime.NewTracker(),
		groupAnnouncements: newGroupAnnouncementTracker(),
		submissionStarts:   a.runtimeOwner.SubmissionStarts,
	}
}

// NewEffectRunner creates the frontend effect executor after the shell and its
// transport have been composed.
func NewEffectRunner(a *App) runtime.EffectRunner { return newEffectRunner(a) }

// NewStateView creates the frontend-scoped state projection.
func NewStateView(a *App) *appstate.Store {
	if a == nil {
		return nil
	}
	view := appstate.NewScoped(a.store, a.FrontendID(), configuredBackend(a), allowLegacyFrontendFallback(a))
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
func CanonicalizeStoredSessionKeys(a *App) error { return canonicalizeStoredSessionKeys(a) }

func BackendKind(a *App) string { return configuredBackend(a) }

func BuildBackendRuntimeHandle(a *App, target string) (*BackendRuntimeHandle, error) {
	return buildBackendRuntimeHandle(a, target)
}

func InstallBackendRuntime(a *App, handle *BackendRuntimeHandle) {
	if handle != nil {
		handle.install(a)
	}
}

func AttachTrackers(a *App, trackers *Trackers) {
	if a != nil && trackers != nil {
		a.registry.Set("trackers", trackers)
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
		a.registry.Set("workspaceRender", presentation)
	}
}

func AttachDispatcher(a *App, dispatcher application.Dispatcher) {
	if a != nil && a.runtimeOwner != nil {
		a.runtimeOwner.Dispatcher = &dispatcher
	}
}

// InstallFeishuHandlers is the final thin-entrypoint step. It binds incoming
// Feishu events and local group policies to the already composed runtime.
func InstallFeishuHandlers(a *App) {
	if a == nil || a.feishu == nil {
		return
	}
	a.feishu.SetHandlers(a.HandleFeishuMessage, a.HandleCardAction, a.HandleFeishuRecall, a.HandleFeishuReaction)
	configureGroupMessagePolicy(a)
	configureGroupPrimaryEvents(a)
	a.feishu.ConfigureLocalFileLinks("", "")
}
