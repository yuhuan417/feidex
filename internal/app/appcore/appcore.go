// Package appcore provides shared helpers and interfaces used by the app
// orchestrator and its sub-packages. Sub-packages cannot import parent app/,
// so helpers that need *App access go through the ConfigurationSource interface here.
package appcore

import (
	domainbackend "feidex/internal/domain/backend"
	"strings"
	"sync"

	"feidex/internal/config"
	"feidex/internal/state"
)

// NormalizeRuntimeBackend normalizes a backend name to its canonical form.
func NormalizeRuntimeBackend(value string) string {
	return domainbackend.NormalizeBackend(value)
}

type ConfigurationSource interface {
	Config() *config.Config
	ConfigMu() *sync.RWMutex
	Backend() string
	FrontendConfigIndex() int
}
type FrontendIdentity interface{ FrontendID() string }
type WorkspaceSource interface {
	WorkspaceSelectionSource
	ConfigurationSource
	FrontendIdentity
	Store() *state.Store
}

// FeishuConfigUnlocked returns the active Feishu config without acquiring
// ConfigMu. Caller must hold at least a read lock.
func FeishuConfigUnlocked(a ConfigurationSource) *config.FeishuConfig {
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
func FeishuConfig(a ConfigurationSource) *config.FeishuConfig {
	if a == nil {
		return nil
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return FeishuConfigUnlocked(a)
}

// ReplyInThreadEnabled returns the fixed Feishu reply mode.
func ReplyInThreadEnabled(_ ConfigurationSource, _ string) bool {
	return false
}

// DebugAllowFrom returns the debug allow list from Feishu config.
func DebugAllowFrom(a ConfigurationSource) []string {
	cfg := FeishuConfig(a)
	if cfg == nil {
		return nil
	}
	return cfg.DebugAllowFrom
}

// AllowLegacyFrontendFallback returns true if the app has exactly one
// configured frontend, allowing sessions without an explicit frontend ID.
func AllowLegacyFrontendFallback(a ConfigurationSource) bool {
	if a == nil || a.Config() == nil {
		return false
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return len(a.Config().ResolvedFrontends()) == 1
}

// ConfiguredBackend returns the active backend name, checking the runtime
// override first, then falling back to the Feishu config.
func ConfiguredBackend(a ConfigurationSource) string {
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
func CurrentRuntimeBackend(a ConfigurationSource) string {
	if a == nil {
		return ""
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return NormalizeRuntimeBackend(a.Backend())
}

// HasConfiguredBackend returns true if a backend is configured.
func HasConfiguredBackend(a ConfigurationSource) bool {
	return strings.TrimSpace(ConfiguredBackend(a)) != ""
}

// DefaultWorkspaceID returns the default workspace ID from the first
// configured workspace, or "default" if none.
func DefaultWorkspaceID(a ConfigurationSource) string {
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
