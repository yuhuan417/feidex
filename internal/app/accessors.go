package app

import (
	"sync"

	appbackend "feidex/internal/adapter/feishu/backend"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/composition"
	frontendruntime "feidex/internal/runtime"

	"feidex/internal/config"
	"feidex/internal/state"
)

func backendDriverForKind(kind string) appbackend.Driver {
	return appbackend.DriverForKind(kind)
}

// Feishu returns the Feishu client. Sub-packages should define narrow
// interfaces for the methods they need rather than depending on this type.
func (a *App) Feishu() FeishuClient {
	if a == nil {
		return nil
	}
	return a.feishu
}

// Config returns the application configuration.
func (a *App) Config() *config.Config {
	if a == nil {
		return nil
	}
	return a.cfg
}

// Store returns the state store.
func (a *App) Store() *state.Store {
	if a == nil {
		return nil
	}
	return a.store
}

// Backend returns the name of the currently active backend.
func (a *App) Backend() string {
	if a == nil {
		return ""
	}
	return a.backend
}

// BackendDriver returns the driver selected for this frontend runtime.
// Composition code updates it together with the runtime backend; the fallback
// keeps manually constructed test apps working during migration.
func (a *App) BackendDriver() appbackend.Driver {
	if a == nil {
		return appbackend.DriverForKind("")
	}
	if a.backendDriver != nil {
		return a.backendDriver
	}
	kind := a.backend
	if kind == "" {
		kind = configuredBackend(a)
	}
	return appbackend.DriverForKind(kind)
}

// Claude returns the Claude core client.
func (a *App) Claude() ClaudeCore {
	return currentClaudeCore(a)
}

// Codex returns the Codex client.
func (a *App) Codex() CodexClient {
	return getCodex(a)
}

// State returns the frontend-scoped app state store.
func (a *App) State() *appstate.Store {
	if a == nil {
		return nil
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.stateView == nil {
		a.stateView = appstate.NewScoped(a.store, a.FrontendID(), configuredBackend(a), allowLegacyFrontendFallback(a))
		a.stateView.RevisionMutex = a.ConfigMu()
	}
	return a.stateView
}

// BotProfile returns the current frontend's persisted default profile.
func (a *App) BotProfile() *state.BotProfile {
	if a == nil || a.State() == nil {
		return nil
	}
	return a.State().BotProfile()
}

// AgentBindingsForChat returns local binding configuration for one logical
// chat. It is a frontend-scoped capability used by binding-aware helpers.
func (a *App) AgentBindingsForChat(chatType, chatID string) []*state.AgentBinding {
	if a == nil {
		return nil
	}
	st := a.State()
	if st == nil {
		return nil
	}
	return st.AgentBindingsForChat(chatType, chatID)
}

// ConfigPath returns the filesystem path to the configuration file.
func (a *App) ConfigPath() string {
	if a == nil {
		return ""
	}
	return a.cfgPath
}

// FrontendID returns the configured frontend identifier.
func (a *App) FrontendID() string {
	if a == nil {
		return ""
	}
	return a.frontendID
}

// BackendRuntime returns the runtime facade for the currently configured backend.
func (a *App) BackendRuntime() backendRuntimeFacade {
	return backendRuntime(a)
}

func currentClaudeCore(a *App) ClaudeCore {
	if a == nil {
		return nil
	}
	owner := ensureRuntimeOwner(a)
	a.composition.clientsMu.RLock()
	legacy := a.composition.claude
	a.composition.clientsMu.RUnlock()
	if current := owner.ClaudeCore(); current != nil {
		return current
	}
	return legacy
}

func ensureCompositionState(a *App) {
	if a == nil {
		return
	}
	if a.composition == nil {
		a.composition = &appComposition{}
	}
}

// ensureRuntimeOwner binds hand-built legacy fixtures to the frontend-scoped
// runtime owner. New production frontends are initialized by NewFrontend and
// already have this owner.
func ensureRuntimeOwner(a *App) *frontendruntime.FrontendOwner {
	if a == nil {
		return nil
	}
	a.runtimeOwnerMu.Lock()
	defer a.runtimeOwnerMu.Unlock()
	ensureCompositionState(a)
	if a.runtimeOwner != nil {
		return a.runtimeOwner
	}
	a.runtimeOwner = composition.NewFrontendOwner()
	if a.sessionActors != nil {
		a.runtimeOwner.SessionActors = a.sessionActors
	}
	if a.composition.liveThreads != nil {
		a.runtimeOwner.LiveThreads = a.composition.liveThreads
	}
	if a.composition.autoRetries != nil {
		a.runtimeOwner.AutoRetries = a.composition.autoRetries
	}
	if a.composition.codexRecovery != nil {
		a.runtimeOwner.CodexRecovery = a.composition.codexRecovery
	}
	if a.composition.trackers != nil && a.composition.trackers.submissionStarts != nil {
		a.runtimeOwner.SubmissionStarts = a.composition.trackers.submissionStarts
	}
	a.composition.clientsMu.RLock()
	legacyCodex, legacyClaude := a.composition.codex, a.composition.claude
	a.composition.clientsMu.RUnlock()
	if legacyCodex != nil {
		a.runtimeOwner.SetCodexClient(legacyCodex)
	}
	if legacyClaude != nil {
		a.runtimeOwner.SetClaudeCore(legacyClaude)
	}
	a.sessionActors = a.runtimeOwner.SessionActors
	a.composition.liveThreads = a.runtimeOwner.LiveThreads
	a.composition.autoRetries = a.runtimeOwner.AutoRetries
	a.composition.codexRecovery = a.runtimeOwner.CodexRecovery
	if a.composition.trackers != nil {
		a.composition.trackers.submissionStarts = a.runtimeOwner.SubmissionStarts
	}
	return a.runtimeOwner
}

func setCompositionClaude(a *App, core ClaudeCore) {
	if a == nil {
		return
	}
	ensureCompositionState(a)
	a.composition.clientsMu.Lock()
	a.composition.claude = core
	a.composition.clientsMu.Unlock()
	ensureRuntimeOwner(a).SetClaudeCore(core)
}

// Trackers returns the per-service runtime tracker bundle.
func (a *App) Trackers() *appTrackers {
	if a == nil {
		return nil
	}
	ensureCompositionState(a)
	if a.composition.trackers == nil {
		a.composition.trackers = &appTrackers{}
	}
	return a.composition.trackers
}

func submissionStartTracker(a *App) *frontendruntime.SubmissionStarts {
	owner := ensureRuntimeOwner(a)
	if owner == nil {
		return nil
	}
	if owner.SubmissionStarts == nil {
		owner.SubmissionStarts = &frontendruntime.SubmissionStarts{}
	}
	return owner.SubmissionStarts
}

func (a *App) sessionActorRuntime() *frontendruntime.SessionActors {
	if a == nil {
		return nil
	}
	owner := ensureRuntimeOwner(a)
	if owner.SessionActors == nil {
		owner.SessionActors = frontendruntime.NewSessionActors()
	}
	return owner.SessionActors
}

func (a *App) invalidateThreadMenuService() {
	if a == nil {
		return
	}
	if a.composition != nil {
		a.composition.mu.Lock()
		a.composition.threadMenu = nil
		a.composition.mu.Unlock()
	}
}

func (a *App) invalidateBackendConfigurationService() {
	if a == nil {
		return
	}
	if a.composition != nil {
		a.composition.mu.Lock()
		a.composition.backendConfig = nil
		a.composition.mu.Unlock()
		a.composition.workspaceMu.Lock()
		a.composition.workspaceConfig = nil
		a.composition.workspaceManage = nil
		a.composition.workspaceMu.Unlock()
	}
}

// ConfigMu returns the config read-write mutex.
func (a *App) ConfigMu() *sync.RWMutex {
	if a == nil {
		return nil
	}
	return a.configMutex()
}

// FrontendConfigIndex returns the active frontend configuration index.
func (a *App) FrontendConfigIndex() int {
	if a == nil {
		return -1
	}
	return a.frontendConfigIndex
}

// SetBackend sets the runtime backend override.
func (a *App) SetBackend(backend string) {
	if a == nil {
		return
	}
	a.configMutex().Lock()
	defer a.configMutex().Unlock()
	a.backend = normalizeRuntimeBackend(backend)
	a.backendDriver = appbackend.DriverForKind(a.backend)
	a.invalidateThreadMenuService()
	a.invalidateBackendConfigurationService()
	if a.stateView != nil {
		a.stateView.SetBackend(a.backend)
	}
}

// MaintenanceTrackers returns the maintenance tracker map, lazily initializing it.
func (a *App) MaintenanceTrackers() appbackend.TrackerMap {
	if a == nil {
		return nil
	}
	trackers := a.Trackers()
	if trackers.maintenanceTrackers == nil {
		trackers.maintenanceTrackers = make(appbackend.TrackerMap)
	}
	return trackers.maintenanceTrackers
}
