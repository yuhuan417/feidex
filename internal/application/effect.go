package application

import (
	"feidex/internal/domain/identity"
)

// Effect is a side effect produced after a domain/application transition.
// Runtime executes effects after state changes have been persisted.
type Effect interface{ effect() }

// SaveState asks the repository adapter to persist an aggregate snapshot.
// Aggregate and Snapshot stay generic during the first migration phase; each
// application use case will later replace them with its own typed port.
type SaveState struct {
	Frontend  identity.FrontendID
	Aggregate string
	Key       string
	Snapshot  any
}

func (SaveState) effect() {}

// SendMessage is a semantic outbound message. Feishu-specific request types
// are created only inside the Feishu adapter.
type SendMessage struct {
	Frontend       identity.FrontendID
	Chat           identity.ChatRef
	Text           string
	ReplyMessageID string
	InThread       bool
}

func (SendMessage) effect() {}

// PatchCard is a semantic card update. The view is intentionally opaque to
// the transport layer until the presentation package is migrated.
type PatchCard struct {
	Frontend  identity.FrontendID
	MessageID string
	View      any
}

func (PatchCard) effect() {}

// StartTurn describes the backend-neutral part of a locally initiated turn.
// Backend adapters add their protocol-specific request details internally.
type StartTurn struct {
	Frontend   identity.FrontendID
	SessionKey identity.SessionKey
	ThreadID   string
	TurnID     string
	Input      string
}

func (StartTurn) effect() {}

// ResolveBackendRequest asks a backend adapter to resolve one pending request.
type ResolveBackendRequest struct {
	Frontend  identity.FrontendID
	RequestID string
	Decision  string
	Payload   any
}

func (ResolveBackendRequest) effect() {}
