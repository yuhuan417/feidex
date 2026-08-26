package app

import (
	"strings"

	"feidex/internal/app/appcore"
)

func canonicalizeStoredSessionKeys(a *App) error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.CanonicalizeSessionKeys(func(key, chatType, chatID, frontendHint string) string {
		key = strings.TrimSpace(key)
		if isAuxiliarySessionKey(key) {
			return key
		}
		chatID = strings.TrimSpace(chatID)
		parsedFrontendID, _, parsedChatID, _, _ := appcore.ParseSessionKey(key)
		if chatID == "" {
			chatID = strings.TrimSpace(parsedChatID)
		}
		if chatID == "" {
			return key
		}
		frontendID := firstNonEmpty(strings.TrimSpace(parsedFrontendID), strings.TrimSpace(frontendHint))
		if frontendID == "" && appcore.AllowLegacyFrontendFallback(a) {
			frontendID = strings.TrimSpace(a.FrontendID())
		}
		if frontendID == "" {
			return key
		}
		return appcore.CanonicalSessionKey(frontendID, "feishu:chat:"+chatID)
	})
}

func sessionKeysEqual(a *App, left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	return canonicalSessionKeyForApp(a, left, "", "") == canonicalSessionKeyForApp(a, right, "", "")
}

func canonicalSessionKeyForApp(a *App, key, chatType, chatID string) string {
	key = strings.TrimSpace(key)
	if isAuxiliarySessionKey(key) {
		return key
	}
	chatID = strings.TrimSpace(chatID)
	parsedFrontendID, _, parsedChatID, _, _ := appcore.ParseSessionKey(key)
	if chatID == "" {
		chatID = strings.TrimSpace(parsedChatID)
	}
	if chatID == "" {
		return key
	}
	frontendID := strings.TrimSpace(parsedFrontendID)
	if frontendID == "" && a != nil && appcore.AllowLegacyFrontendFallback(a) {
		frontendID = strings.TrimSpace(a.FrontendID())
	}
	return appcore.CanonicalSessionKey(frontendID, "feishu:chat:"+chatID)
}

func isAuxiliarySessionKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.Contains(key, ":workspace:") || strings.Contains(key, ":pending:")
}
