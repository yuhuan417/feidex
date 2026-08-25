// Package appcore provides shared helpers and interfaces used by the app
// orchestrator and its sub-packages. Sub-packages cannot import parent app/,
// so helpers that need *App access go through the AppConfig interface here.
package appcore

import (
	"strings"
	"sync"

	appruntime "feidex/internal/app/runtime"
	"feidex/internal/config"
	"feidex/internal/state"
)

// NormalizeRuntimeBackend normalizes a backend name to its canonical form.
func NormalizeRuntimeBackend(value string) string {
	return appruntime.NormalizeBackend(value)
}

// AppConfig is the narrow interface that shared helpers use to access
// *App fields. *App satisfies this via its accessor methods.
type AppConfig interface {
	Config() *config.Config
	ConfigMu() *sync.RWMutex
	Backend() string
	FrontendID() string
	FrontendConfigIndex() int
	Store() *state.Store
}

// AppExtended adds mutation and runtime methods needed by sub-packages
// that manage backend state. Not all sub-packages need all methods.
type AppExtended interface {
	AppConfig
	SetBackend(backend string)
	ConfigPath() string
}

// FeishuConfigUnlocked returns the active Feishu config without acquiring
// ConfigMu. Caller must hold at least a read lock.
func FeishuConfigUnlocked(a AppConfig) *config.FeishuConfig {
	if a == nil || a.Config() == nil {
		return nil
	}
	cfg := a.Config()
	idx := a.FrontendConfigIndex()
	if idx >= 0 && idx < len(cfg.Frontends) {
		return &cfg.Frontends[idx].FeishuConfig
	}
	return &cfg.Feishu
}

// FeishuConfig returns the active Feishu config, acquiring ConfigMu.
func FeishuConfig(a AppConfig) *config.FeishuConfig {
	if a == nil {
		return nil
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return FeishuConfigUnlocked(a)
}

// ReplyInThreadEnabled returns the fixed Feishu reply mode.
func ReplyInThreadEnabled(_ AppConfig, _ string) bool {
	return false
}

// DebugAllowFrom returns the debug allow list from Feishu config.
func DebugAllowFrom(a AppConfig) []string {
	cfg := FeishuConfig(a)
	if cfg == nil {
		return nil
	}
	return cfg.DebugAllowFrom
}

// AllowLegacyFrontendFallback returns true if the app has exactly one
// configured frontend, allowing sessions without an explicit frontend ID.
func AllowLegacyFrontendFallback(a AppConfig) bool {
	if a == nil || a.Config() == nil {
		return false
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return len(a.Config().ResolvedFrontends()) == 1
}

// ConfiguredBackend returns the active backend name, checking the runtime
// override first, then falling back to the Feishu config.
func ConfiguredBackend(a AppConfig) string {
	if a == nil {
		return ""
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	if backend := NormalizeRuntimeBackend(a.Backend()); strings.TrimSpace(a.Backend()) != "" {
		return backend
	}
	if cfg := FeishuConfigUnlocked(a); cfg != nil {
		return NormalizeRuntimeBackend(cfg.Backend)
	}
	return ""
}

// CurrentRuntimeBackend returns the raw runtime backend override (normalized).
func CurrentRuntimeBackend(a AppConfig) string {
	if a == nil {
		return ""
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return NormalizeRuntimeBackend(a.Backend())
}

// HasConfiguredBackend returns true if a backend is configured.
func HasConfiguredBackend(a AppConfig) bool {
	return strings.TrimSpace(ConfiguredBackend(a)) != ""
}

// DefaultWorkspaceID returns the default workspace ID from the first
// configured workspace, or "default" if none.
func DefaultWorkspaceID(a AppConfig) string {
	if a == nil || a.Config() == nil {
		return "default"
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	cfg := a.Config()
	if len(cfg.Workspaces) == 0 {
		return "default"
	}
	return cfg.Workspaces[0].ID
}
