package feishuapp

import (
	"feidex/internal/domain/identity"
	"feidex/internal/state"
	"feidex/internal/textutil"
	"strings"
)

func canonicalizeStoredSessionKeys(store *state.Store) error {
	if store == nil {
		return nil
	}
	return store.CanonicalizeSessionKeys(func(key, chatType, chatID, frontendHint string) string {
		key = strings.TrimSpace(key)
		if isAuxiliarySessionKey(key) {
			return key
		}
		chatID = strings.TrimSpace(chatID)
		parsedFrontendID, _, parsedChatID, _, _ := identity.ParseSessionKey(key)
		if chatID == "" {
			chatID = strings.TrimSpace(parsedChatID)
		}
		if chatID == "" {
			return key
		}
		frontendID := textutil.FirstNonEmpty(strings.TrimSpace(parsedFrontendID), strings.TrimSpace(frontendHint))
		if frontendID == "" {
			return key
		}
		return identity.CanonicalSessionKey(frontendID, "feishu:chat:"+chatID)
	})
}

func sessionKeysEqual(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	return canonicalStoredSessionKey(left) == canonicalStoredSessionKey(right)
}

func canonicalStoredSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if isAuxiliarySessionKey(key) {
		return key
	}
	parsedFrontendID, _, parsedChatID, _, _ := identity.ParseSessionKey(key)
	chatID := strings.TrimSpace(parsedChatID)
	if chatID == "" {
		return key
	}
	frontendID := strings.TrimSpace(parsedFrontendID)
	return identity.CanonicalSessionKey(frontendID, "feishu:chat:"+chatID)
}

func isAuxiliarySessionKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.Contains(key, ":workspace:") || strings.Contains(key, ":pending:")
}
