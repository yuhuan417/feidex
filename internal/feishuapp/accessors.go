package feishuapp

import (
	"sync"

	appbackend "feidex/internal/adapter/feishu/backend"
	appstate "feidex/internal/adapter/storage/json/scoped"
	frontendruntime "feidex/internal/runtime"

	"feidex/internal/config"
	"feidex/internal/state"
)

// Feishu returns the Feishu client. Sub-packages should define narrow
// interfaces for the methods they need rather than depending on this type.
// AsyncRunner exposes the frontend's async executor so composition can hand
// it to components that need to schedule work off the callback path.
func (a *App) AsyncRunner() func(func()) {
	if a == nil {
		return nil
	}
	return a.asyncRunner
}

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
	return a.runtimeOwner.Backend()
}

// BackendDriver follows the frontend backend selected at execution time.
func (a *App) BackendDriver() appbackend.Driver {
	return appbackend.SelectedDriver{Selected: func() string { return a.configView().configuredBackend() }}
}

// State returns the frontend-scoped app state store.
func (a *App) State() *appstate.Store {
	if a == nil {
		return nil
	}
	return a.stateView
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

func (a *App) sessionActorRuntime() *frontendruntime.SessionActors {
	if a == nil {
		return nil
	}
	return a.runtimeOwner.SessionActors
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
	a.runtimeOwner.SetBackend(normalizeRuntimeBackend(backend))
	if a.stateView != nil {
		a.stateView.SetBackend(a.runtimeOwner.Backend())
	}
}
