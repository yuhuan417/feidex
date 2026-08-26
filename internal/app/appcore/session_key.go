package appcore

import (
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

// ParseSessionKey parses a session key into its component parts. The current
// canonical shape is feishu:frontend:<frontend_id>:chat:<chat_id>; older typed
// shapes are still parsed so persisted state and old card payloads can be
// normalized.
func ParseSessionKey(sessionKey string) (frontendID, chatType, chatID, rootMessageID, userID string) {
	parts := strings.Split(strings.TrimSpace(sessionKey), ":")
	if len(parts) < 2 || parts[0] != "feishu" {
		return "", "", "", "", ""
	}
	if len(parts) == 2 {
		return "", "", strings.TrimSpace(parts[1]), "", ""
	}
	if parts[1] == "chat" {
		if len(parts) > 2 {
			return "", "", strings.TrimSpace(parts[2]), "", ""
		}
		return "", "", "", "", ""
	}
	if len(parts) >= 3 && parts[1] != "frontend" && parts[1] != "group" && parts[1] != "p2p" {
		return strings.TrimSpace(parts[1]), "", strings.TrimSpace(parts[2]), "", ""
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
	case "chat":
		if len(parts) > offset+1 {
			return frontendID, "", strings.TrimSpace(parts[offset+1]), "", ""
		}
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

// CanonicalSessionKey normalizes a Feishu session key to the current
// frontend/chat identity shape. Legacy p2p/group/root/user segments are parsed
// for compatibility but never kept in the returned key.
func CanonicalSessionKey(frontendID, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	parsedFrontendID, _, chatID, _, _ := ParseSessionKey(sessionKey)
	frontendID = strings.TrimSpace(FirstNonEmpty(parsedFrontendID, frontendID))
	if chatID == "" {
		return sessionKey
	}
	if frontendID != "" {
		return fmt.Sprintf("feishu:frontend:%s:chat:%s", frontendID, chatID)
	}
	return fmt.Sprintf("feishu:chat:%s", chatID)
}

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
	return CanonicalSessionKey(frontendID, sessionKey)
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
	frontendID, _, _, _, _ := ParseSessionKey(sessionKey)
	if strings.TrimSpace(frontendID) == strings.TrimSpace(a.FrontendID()) {
		return true
	}
	return frontendID == "" && AllowLegacyFrontendFallback(a)
}
