package feishuapp

import (
	"strings"
	"sync"

	"feidex/internal/config"
	"feidex/internal/domain/backend"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

func normalizeRuntimeBackend(value string) string { return backend.NormalizeBackend(value) }

// frontendConfigView is the only thing these helpers ever read off the
// frontend aggregate: its configuration, the mutex guarding it, the
// frontend's identity, and which frontend entry of the config this scope is.
//
// They used to take *App and reach for a.Config(), a.ConfigMu(), a.Backend(),
// a.FrontendID() and a.FrontendConfigIndex(). Taking this view instead lets
// the port factories capture five values rather than the aggregate.
type frontendConfigView struct {
	cfg                 *config.Config
	mu                  *sync.RWMutex
	backend             string
	frontendID          string
	frontendConfigIndex int
}

// configView snapshots the values above. It is intentionally cheap and
// lock-free: everything it reads is fixed after construction apart from the
// config pointer itself, which the helpers below guard with mu.
func (a *App) configView() frontendConfigView {
	if a == nil {
		return frontendConfigView{}
	}
	// Backend is resolved here rather than stored: it follows the runtime
	// owner's current selection, and every caller snapshots the view
	// immediately before using it.
	return frontendConfigView{
		cfg:                 a.cfg,
		mu:                  a.ConfigMu(),
		backend:             a.Backend(),
		frontendID:          a.frontendID,
		frontendConfigIndex: a.frontendConfigIndex,
	}
}

func (v frontendConfigView) feishuConfigUnlocked() *config.FeishuConfig {
	if v.cfg == nil {
		return nil
	}
	if v.frontendConfigIndex >= 0 && v.frontendConfigIndex < len(v.cfg.Frontends) {
		return &v.cfg.Frontends[v.frontendConfigIndex].FeishuConfig
	}
	return &v.cfg.Feishu
}

func (v frontendConfigView) feishuConfig() *config.FeishuConfig {
	if v.cfg == nil {
		return nil
	}
	if v.mu == nil {
		return v.feishuConfigUnlocked()
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.feishuConfigUnlocked()
}

// replyInThreadEnabled reports whether replies open a thread. The frontend
// config carries no such switch, so this is always false.
func (frontendConfigView) replyInThreadEnabled() bool { return false }

func (v frontendConfigView) configuredBackend() string {
	if v.mu == nil {
		return ""
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if strings.TrimSpace(v.backend) != "" {
		return normalizeRuntimeBackend(v.backend)
	}
	if cfg := v.feishuConfigUnlocked(); cfg != nil {
		return normalizeRuntimeBackend(cfg.Backend)
	}
	return ""
}

func (v frontendConfigView) hasConfiguredBackend() bool {
	return strings.TrimSpace(v.configuredBackend()) != ""
}

func (v frontendConfigView) defaultWorkspaceID() string {
	if v.cfg == nil || v.mu == nil {
		return "default"
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if len(v.cfg.Workspaces) == 0 {
		return "default"
	}
	return v.cfg.Workspaces[0].ID
}

func (v frontendConfigView) normalizeSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	return identity.CanonicalSessionKey(strings.TrimSpace(v.frontendID), sessionKey)
}

func (v frontendConfigView) sessionBelongsToFrontend(sessionKey string) bool {
	return sessionBelongsToFrontend(v.frontendID, sessionKey)
}

func sessionBelongsToFrontend(frontendID, sessionKey string) bool {
	sessionFrontendID, _, _, _, _ := identity.ParseSessionKey(sessionKey)
	return strings.TrimSpace(sessionFrontendID) == strings.TrimSpace(frontendID)
}

func (v frontendConfigView) makeSessionKey(msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.SessionKey) != "" {
		return v.normalizeSessionKey(msg.SessionKey)
	}
	frontendID := strings.TrimSpace(v.frontendID)
	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return ""
	}
	if frontendID != "" {
		return "feishu:frontend:" + frontendID + ":chat:" + chatID
	}
	return "feishu:chat:" + chatID
}

func SessionKeyBuilder(frontendID string) func(*feishu.InboundMessage) string {
	view := frontendConfigView{frontendID: frontendID}
	return view.makeSessionKey
}

func ConfiguredBackendBuilder(cfg *config.Config, mu *sync.RWMutex, backend func() string, frontendID string, frontendConfigIndex int) func() string {
	return func() string {
		active := ""
		if backend != nil {
			active = backend()
		}
		return (frontendConfigView{
			cfg: cfg, mu: mu, backend: active,
			frontendID: frontendID, frontendConfigIndex: frontendConfigIndex,
		}).configuredBackend()
	}
}
