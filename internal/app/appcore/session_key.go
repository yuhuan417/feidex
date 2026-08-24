package appcore

import (
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

// ParseSessionKey parses a session key into its component parts.
func ParseSessionKey(sessionKey string) (frontendID, chatType, chatID, rootMessageID, userID string) {
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
		if len(parts) > offset+3 {
			if parts[offset+2] == "root" {
				return frontendID, "group", strings.TrimSpace(parts[offset+1]), strings.TrimSpace(parts[offset+3]), ""
			}
		}
	case "p2p":
		if len(parts) > offset+2 {
			return frontendID, "p2p", strings.TrimSpace(parts[offset+1]), "", strings.TrimSpace(parts[offset+2])
		}
	}
	return frontendID, "", "", "", ""
}

// NormalizeSessionKey ensures the session key includes the frontend ID prefix.
func NormalizeSessionKey(a AppConfig, sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if frontendID, _, _, _, _ := ParseSessionKey(sessionKey); frontendID != "" || strings.TrimSpace(a.FrontendID()) == "" {
		return sessionKey
	}
	_, chatType, chatID, rootMessageID, userID := ParseSessionKey(sessionKey)
	switch chatType {
	case "group":
		if chatID == "" || rootMessageID == "" {
			return sessionKey
		}
		return "feishu:frontend:" + strings.TrimSpace(a.FrontendID()) + ":group:" + chatID + ":root:" + rootMessageID
	case "p2p":
		if chatID == "" || userID == "" {
			return sessionKey
		}
		return "feishu:frontend:" + strings.TrimSpace(a.FrontendID()) + ":p2p:" + chatID + ":" + userID
	default:
		return sessionKey
	}
}

// MakeSessionKey builds a session key from an inbound message.
func MakeSessionKey(a AppConfig, msg *feishu.InboundMessage) string {
	if msg != nil && strings.TrimSpace(msg.SessionKey) != "" {
		return NormalizeSessionKey(a, msg.SessionKey)
	}
	frontendID := strings.TrimSpace(a.FrontendID())
	if msg.ChatType == "group" {
		root := msg.RootMessageID
		if root == "" {
			root = msg.MessageID
		}
		if frontendID != "" {
			return fmt.Sprintf("feishu:frontend:%s:group:%s:root:%s", frontendID, msg.ChatID, root)
		}
		return fmt.Sprintf("feishu:group:%s:root:%s", msg.ChatID, root)
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
