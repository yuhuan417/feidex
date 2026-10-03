package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/domain/backend"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

func normalizeRuntimeBackend(value string) string { return backend.NormalizeBackend(value) }

func feishuConfig(a *App) *config.FeishuConfig {
	if a == nil || a.Config() == nil {
		return nil
	}
	mu := a.ConfigMu()
	if mu == nil {
		return feishuConfigUnlocked(a)
	}
	mu.RLock()
	defer mu.RUnlock()
	return feishuConfigUnlocked(a)
}

func feishuConfigUnlocked(a *App) *config.FeishuConfig {
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

func replyInThreadEnabled(_ *App, _ string) bool { return false }

func configuredBackend(a *App) string {
	if a == nil || a.ConfigMu() == nil {
		return ""
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	if strings.TrimSpace(a.Backend()) != "" {
		return normalizeRuntimeBackend(a.Backend())
	}
	if cfg := feishuConfigUnlocked(a); cfg != nil {
		return normalizeRuntimeBackend(cfg.Backend)
	}
	return ""
}

func hasConfiguredBackend(a *App) bool { return strings.TrimSpace(configuredBackend(a)) != "" }

func defaultWorkspaceID(a *App) string {
	if a == nil || a.Config() == nil || a.ConfigMu() == nil {
		return "default"
	}
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	if len(a.Config().Workspaces) == 0 {
		return "default"
	}
	return a.Config().Workspaces[0].ID
}

func normalizeSessionKey(a *App, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	frontendID := ""
	if a != nil {
		frontendID = a.FrontendID()
	}
	return identity.CanonicalSessionKey(frontendID, sessionKey)
}

func sessionBelongsToFrontend(a *App, sessionKey string) bool {
	frontendID, _, _, _, _ := identity.ParseSessionKey(sessionKey)
	if a == nil {
		return false
	}
	return strings.TrimSpace(frontendID) == strings.TrimSpace(a.FrontendID()) || (frontendID == "" && allowLegacyFrontendFallback(a))
}

func makeSessionKey(a *App, msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.SessionKey) != "" {
		return normalizeSessionKey(a, msg.SessionKey)
	}
	frontendID := ""
	if a != nil {
		frontendID = strings.TrimSpace(a.FrontendID())
	}
	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return ""
	}
	if frontendID != "" {
		return "feishu:frontend:" + frontendID + ":chat:" + chatID
	}
	return "feishu:chat:" + chatID
}
