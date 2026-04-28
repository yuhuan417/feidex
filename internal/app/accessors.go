package app

import (
	"sync"

	"feidex/internal/app/appcore"
	"feidex/internal/app/appstate"
	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/state"
)

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

// Claude returns the Claude core client.
func (a *App) Claude() ClaudeCore {
	if a == nil {
		return nil
	}
	return a.claude
}

// Codex returns the Codex client.
func (a *App) Codex() CodexClient {
	if a == nil {
		return nil
	}
	return a.codex
}

// State returns the frontend-scoped app state store.
func (a *App) State() *appstate.Store {
	if a == nil {
		return nil
	}
	return appstate.New(a)
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

// ConversationBackend returns the conversation backend facade for the active backend.
func (a *App) ConversationBackend() conversationBackendFacade {
	return conversationBackend(a)
}

// Trackers returns the per-service runtime tracker bundle.
func (a *App) Trackers() *appTrackers {
	if a == nil {
		return nil
	}
	return &a.trackers
}

// ConfigMu returns the config read-write mutex.
func (a *App) ConfigMu() *sync.RWMutex {
	if a == nil {
		return nil
	}
	return &a.configMu
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
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.backend = appcore.NormalizeRuntimeBackend(backend)
}

// BackendStateMu returns the backend state mutex.
func (a *App) BackendStateMu() *sync.Mutex {
	if a == nil {
		return nil
	}
	return &a.backendStateMu
}

// BackendSwitchMu returns the backend switch mutex.
func (a *App) BackendSwitchMu() *sync.Mutex {
	if a == nil {
		return nil
	}
	return &a.backendSwitchMu
}

// BackendSwitching returns whether a backend switch is in progress.
func (a *App) BackendSwitching() bool {
	if a == nil {
		return false
	}
	return a.backendSwitching
}

// SetBackendSwitching sets the backend switching flag.
func (a *App) SetBackendSwitching(v bool) {
	if a == nil {
		return
	}
	a.backendSwitching = v
}

// BackendSwitchTarget returns the target backend during a switch.
func (a *App) BackendSwitchTarget() string {
	if a == nil {
		return ""
	}
	return a.backendSwitchTarget
}

// SetBackendSwitchTarget sets the target backend during a switch.
func (a *App) SetBackendSwitchTarget(v string) {
	if a == nil {
		return
	}
	a.backendSwitchTarget = v
}

// DefaultWorkspaceID returns the default workspace ID.
func (a *App) DefaultWorkspaceID() string {
	return appcore.DefaultWorkspaceID(a)
}

// MaintenanceTrackers returns the maintenance tracker map, lazily initializing it.
func (a *App) MaintenanceTrackers() appbackend.TrackerMap {
	if a == nil {
		return nil
	}
	if a.trackers.maintenanceTrackers == nil {
		a.trackers.maintenanceTrackers = make(appbackend.TrackerMap)
	}
	return a.trackers.maintenanceTrackers
}
