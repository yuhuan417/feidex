package app

import (
	"context"
	"strings"

	"feidex/internal/config"
)

func startBackend(a *App, ctx context.Context) error {
	if a == nil {
		return nil
	}
	return startPreparedBackendRuntime(a, ctx, currentBackendRuntimeHandle(a))
}

func startFrontend(a *App, ctx context.Context) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.Start(ctx)
}

func feishuConfigUnlocked(a *App) *config.FeishuConfig {
	if a == nil || a.cfg == nil {
		return nil
	}
	if a.frontendConfigIndex >= 0 && a.frontendConfigIndex < len(a.cfg.Frontends) {
		return &a.cfg.Frontends[a.frontendConfigIndex].FeishuConfig
	}
	return &a.cfg.Feishu
}

func feishuConfig(a *App) *config.FeishuConfig {
	if a == nil {
		return nil
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return feishuConfigUnlocked(a)
}

func replyInThreadEnabled(a *App, chatType string) bool {
	cfg := feishuConfig(a)
	return cfg != nil && strings.TrimSpace(chatType) == "group" && cfg.ReplyInThread
}

func debugAllowFrom(a *App) []string {
	cfg := feishuConfig(a)
	if cfg == nil {
		return nil
	}
	return cfg.DebugAllowFrom
}

func allowLegacyFrontendFallback(a *App) bool {
	if a == nil || a.cfg == nil {
		return false
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return len(a.cfg.ResolvedFrontends()) == 1
}

func sessionBelongsToFrontend(a *App, sessionKey string) bool {
	frontendID, _, _, _, _ := parseSessionKey(sessionKey)
	if strings.TrimSpace(frontendID) == strings.TrimSpace(a.frontendID) {
		return true
	}
	return frontendID == "" && allowLegacyFrontendFallback(a)
}

func normalizeSessionKey(a *App, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if frontendID, _, _, _, _ := parseSessionKey(sessionKey); frontendID != "" || strings.TrimSpace(a.frontendID) == "" {
		return sessionKey
	}
	_, chatType, chatID, rootMessageID, userID := parseSessionKey(sessionKey)
	switch chatType {
	case "group":
		if chatID == "" || rootMessageID == "" {
			return sessionKey
		}
		return "feishu:frontend:" + strings.TrimSpace(a.frontendID) + ":group:" + chatID + ":root:" + rootMessageID
	case "p2p":
		if chatID == "" || userID == "" {
			return sessionKey
		}
		return "feishu:frontend:" + strings.TrimSpace(a.frontendID) + ":p2p:" + chatID + ":" + userID
	default:
		return sessionKey
	}
}

func parseSessionKey(sessionKey string) (frontendID, chatType, chatID, rootMessageID, userID string) {
	parts := strings.Split(strings.TrimSpace(sessionKey), ":")
	if len(parts) < 4 || parts[0] != "feishu" {
		return "", "", "", "", ""
	}
	offset := 1
	if len(parts) >= 6 && parts[1] == "frontend" {
		frontendID = strings.TrimSpace(parts[2])
		offset = 3
	}
	switch parts[offset] {
	case "group":
		if len(parts) > offset+3 && parts[offset+2] == "root" {
			return frontendID, "group", strings.TrimSpace(parts[offset+1]), strings.TrimSpace(parts[offset+3]), ""
		}
	case "p2p":
		if len(parts) > offset+2 {
			return frontendID, "p2p", strings.TrimSpace(parts[offset+1]), "", strings.TrimSpace(parts[offset+2])
		}
	}
	return frontendID, "", "", "", ""
}
