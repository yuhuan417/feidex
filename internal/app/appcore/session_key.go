package appcore

import (
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

// ParseSessionKey parses a session key into its component parts.
func ParseSessionKey(sessionKey string) (frontendID, chatType, chatID, rootMessageID, userID string) {
	parts := strings.Split(strings.TrimSpace(sessionKey), ":")
	if len(parts) < 3 || parts[0] != "feishu" {
		return "", "", "", "", ""
	}
	offset := 1
	if len(parts) > 3 && parts[1] == "frontend" {
		frontendID = strings.TrimSpace(parts[2])
		offset = 3
	}
	if offset >= len(parts) {
		return frontendID, "", "", "", ""
	}
	switch parts[offset] {
	case "group":
		if len(parts) <= offset+1 || strings.TrimSpace(parts[offset+1]) == "" {
			return frontendID, "", "", "", ""
		}
		if len(parts) > offset+3 && parts[offset+2] == "root" {
			return frontendID, "group", strings.TrimSpace(parts[offset+1]), strings.TrimSpace(parts[offset+3]), ""
		}
		return frontendID, "group", strings.TrimSpace(parts[offset+1]), "", ""
	case "p2p":
		if len(parts) > offset+2 {
			return frontendID, "p2p", strings.TrimSpace(parts[offset+1]), "", strings.TrimSpace(parts[offset+2])
		}
	}
	return frontendID, "", "", "", ""
}

// CanonicalSessionKey normalizes a Feishu session key to the current session
// identity shape. Group sessions are scoped only by frontend and chat; legacy
// root segments are parsed for compatibility but never kept in the returned key.
func CanonicalSessionKey(frontendID, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	parsedFrontendID, chatType, chatID, _, userID := ParseSessionKey(sessionKey)
	frontendID = strings.TrimSpace(FirstNonEmpty(parsedFrontendID, frontendID))
	switch chatType {
	case "group":
		if chatID == "" {
			return sessionKey
		}
		if frontendID != "" {
			return fmt.Sprintf("feishu:frontend:%s:group:%s", frontendID, chatID)
		}
		return fmt.Sprintf("feishu:group:%s", chatID)
	case "p2p":
		if chatID == "" || userID == "" {
			return sessionKey
		}
		if frontendID != "" {
			return fmt.Sprintf("feishu:frontend:%s:p2p:%s:%s", frontendID, chatID, userID)
		}
		return fmt.Sprintf("feishu:p2p:%s:%s", chatID, userID)
	default:
		return sessionKey
	}
}

// NormalizeSessionKey ensures the session key includes the frontend ID prefix
// and uses the canonical frontend/chat identity for group sessions.
func NormalizeSessionKey(a AppConfig, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	frontendID := ""
	if a != nil {
		frontendID = a.FrontendID()
	}
	return CanonicalSessionKey(frontendID, sessionKey)
}

// MakeSessionKey builds a session key from an inbound message.
func MakeSessionKey(a AppConfig, msg *feishu.InboundMessage) string {
	if msg != nil && strings.TrimSpace(msg.SessionKey) != "" {
		return NormalizeSessionKey(a, msg.SessionKey)
	}
	frontendID := strings.TrimSpace(a.FrontendID())
	if msg.ChatType == "group" {
		if frontendID != "" {
			return fmt.Sprintf("feishu:frontend:%s:group:%s", frontendID, msg.ChatID)
		}
		return fmt.Sprintf("feishu:group:%s", msg.ChatID)
	}
	if frontendID != "" {
		return fmt.Sprintf("feishu:frontend:%s:p2p:%s:%s", frontendID, msg.ChatID, msg.UserID)
	}
	return fmt.Sprintf("feishu:p2p:%s:%s", msg.ChatID, msg.UserID)
}

// SessionBelongsToFrontend returns true if the session key belongs to the
// current frontend (or if legacy fallback allows it).
func SessionBelongsToFrontend(a AppConfig, sessionKey string) bool {
	frontendID, _, _, _, _ := ParseSessionKey(sessionKey)
	if strings.TrimSpace(frontendID) == strings.TrimSpace(a.FrontendID()) {
		return true
	}
	return frontendID == "" && AllowLegacyFrontendFallback(a)
}
