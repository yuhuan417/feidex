package app

import (
	"context"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) startBackend(ctx context.Context) error {
	if a == nil {
		return nil
	}
	return a.startPreparedBackendRuntime(ctx, a.currentBackendRuntimeHandle())
}

func (a *App) startFrontend(ctx context.Context) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.Start(ctx)
}

func (a *App) feishuConfig() *config.FeishuConfig {
	if a == nil || a.cfg == nil {
		return nil
	}
	if a.frontendConfigIndex >= 0 && a.frontendConfigIndex < len(a.cfg.Frontends) {
		return &a.cfg.Frontends[a.frontendConfigIndex].FeishuConfig
	}
	return &a.cfg.Feishu
}

func (a *App) replyInThreadEnabled(chatType string) bool {
	cfg := a.feishuConfig()
	return cfg != nil && strings.TrimSpace(chatType) == "group" && cfg.ReplyInThread
}

func (a *App) debugAllowFrom() []string {
	cfg := a.feishuConfig()
	if cfg == nil {
		return nil
	}
	return cfg.DebugAllowFrom
}

func (a *App) allowLegacyFrontendFallback() bool {
	if a == nil || a.cfg == nil {
		return false
	}
	return len(a.cfg.ResolvedFrontends()) == 1
}

func (a *App) sessionBelongsToFrontend(sessionKey string) bool {
	frontendID, _, _, _, _ := parseSessionKey(sessionKey)
	if strings.TrimSpace(frontendID) == strings.TrimSpace(a.frontendID) {
		return true
	}
	return frontendID == "" && a.allowLegacyFrontendFallback()
}

func (a *App) pendingBelongsToFrontend(req *state.PendingRequest) bool {
	if req == nil {
		return false
	}
	if strings.TrimSpace(req.FrontendID) == strings.TrimSpace(a.frontendID) {
		return true
	}
	if strings.TrimSpace(req.FrontendID) != "" {
		return false
	}
	if strings.TrimSpace(req.SessionKey) != "" {
		return a.sessionBelongsToFrontend(req.SessionKey)
	}
	return a.allowLegacyFrontendFallback()
}

func (a *App) normalizeSessionKey(sessionKey string) string {
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
