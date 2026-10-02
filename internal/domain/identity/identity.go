// Package identity contains transport-independent identifiers used to scope
// Feidex state and runtime work.
package identity

import "strings"

// FrontendID identifies one Feishu frontend and its isolated backend runtime.
type FrontendID string

// ChatType identifies the kind of Feishu conversation.
type ChatType string

const (
	ChatTypeP2P   ChatType = "p2p"
	ChatTypeGroup ChatType = "group"
)

// ChatRef identifies a chat without depending on a Feishu SDK type.
type ChatRef struct {
	Type ChatType
	ID   string
}

// Normalize trims transport input at the domain boundary.
func (id FrontendID) Normalize() FrontendID {
	return FrontendID(strings.TrimSpace(string(id)))
}

// Normalize trims transport input at the domain boundary.
func (chat ChatRef) Normalize() ChatRef {
	chat.Type = ChatType(strings.ToLower(strings.TrimSpace(string(chat.Type))))
	chat.ID = strings.TrimSpace(chat.ID)
	return chat
}

// Valid reports whether both parts of a chat identity are present.
func (chat ChatRef) Valid() bool {
	chat = chat.Normalize()
	return chat.ID != "" && (chat.Type == ChatTypeP2P || chat.Type == ChatTypeGroup)
}

// SessionKey is the stable key used to address one conversation session.
// Construction remains an application concern because legacy session-key
// formats are still owned by the current app layer during migration.
type SessionKey string
