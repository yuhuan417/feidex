package appcore

import (
	"feidex/internal/domain/identity"
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

// NormalizeSessionKey ensures the session key includes the frontend ID prefix
// and uses the canonical frontend/chat identity.
func NormalizeSessionKey(a AppConfig, sessionKey string) string {
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

// MakeSessionKey builds a session key from an inbound message.
func MakeSessionKey(a AppConfig, msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	if msg != nil && strings.TrimSpace(msg.SessionKey) != "" {
		return NormalizeSessionKey(a, msg.SessionKey)
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
		return fmt.Sprintf("feishu:frontend:%s:chat:%s", frontendID, chatID)
	}
	return fmt.Sprintf("feishu:chat:%s", chatID)
}

// SessionBelongsToFrontend returns true if the session key belongs to the
// current frontend (or if legacy fallback allows it).
func SessionBelongsToFrontend(a AppConfig, sessionKey string) bool {
	frontendID, _, _, _, _ := identity.ParseSessionKey(sessionKey)
	if strings.TrimSpace(frontendID) == strings.TrimSpace(a.FrontendID()) {
		return true
	}
	return frontendID == "" && AllowLegacyFrontendFallback(a)
}

var ParseSessionKey = identity.ParseSessionKey
var CanonicalSessionKey = identity.CanonicalSessionKey
