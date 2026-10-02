package application

import (
	"feidex/internal/application/backendops"
	"feidex/internal/application/presentation"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
)

// Effect is a side effect produced after a domain/application transition.
// Runtime executes effects after state changes have been persisted.
type Effect interface{ effect() }

// SaveState persists a conversation snapshot before further effects run.
type SaveState struct {
	Frontend identity.FrontendID
	Session  *conversation.Session
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

// SendCard is a semantic outbound card. The Feishu adapter decides whether
// this becomes a reply or a new message from the target fields.
type SendCard struct {
	Frontend       identity.FrontendID
	Chat           identity.ChatRef
	ReplyMessageID string
	View           presentation.CardView
	InThread       bool
}

func (SendCard) effect() {}

// PatchCard is a semantic card update. The view is intentionally opaque to
// the transport layer until the presentation package is migrated.
type PatchCard struct {
	Frontend  identity.FrontendID
	MessageID string
	View      presentation.CardView
}

func (PatchCard) effect() {}

// StartTurn starts local work using settings captured at the startup boundary.
type StartTurn struct {
	Frontend   identity.FrontendID
	SessionKey identity.SessionKey
	Request    backendops.StartTurnRequest
}

func (StartTurn) effect() {}

// ResolveBackendRequest sends a response to the backend. It does not mark a
// Codex interaction resolved; serverRequest/resolved remains authoritative.
type ResolveBackendRequest struct {
	Frontend identity.FrontendID
	Backend  string
	Response backendops.Response
}

func (ResolveBackendRequest) effect() {}

// SteerTurn targets the already-active turn; it never applies new model settings.
type SteerTurn struct {
	Frontend                                   identity.FrontendID
	SessionKey, ThreadID, ExpectedTurnID, Text string
}

func (SteerTurn) effect() {}

type EnqueueInput struct {
	Frontend            identity.FrontendID
	SessionKey          string
	BindOnlyCurrentRoot bool
	Message             InboundMessage
}

func (EnqueueInput) effect() {}

// RefreshGroupStatus schedules display refresh after a persisted binding change.
type RefreshGroupStatus struct {
	Frontend       identity.FrontendID
	ChatID, Reason string
}

func (RefreshGroupStatus) effect() {}
