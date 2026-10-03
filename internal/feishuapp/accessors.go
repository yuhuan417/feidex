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
	return appbackend.SelectedDriver{Selected: func() string { return configuredBackend(a) }}
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
func (a *App) BackendRuntime() frontendruntime.BackendFacade {
	return backendRuntime(a)
}

func currentClaudeCore(a *App) ClaudeCore {
	if a == nil {
		return nil
	}
	return ensureRuntimeOwner(a).ClaudeCore()
}

func ensureRuntimeOwner(a *App) *frontendruntime.FrontendOwner {
	if a == nil {
		return nil
	}
	return a.runtimeOwner
}

func setClaudeCore(a *App, core ClaudeCore) {
	if a == nil {
		return
	}
	ensureRuntimeOwner(a).SetClaudeCore(core)
}

func submissionStartTracker(a *App) *frontendruntime.SubmissionStarts {
	if a == nil {
		return nil
	}
	return a.runtimeOwner.SubmissionStarts
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
