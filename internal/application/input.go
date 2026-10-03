// Package application contains transport-independent use-case contracts.
// Concrete adapters translate their protocol objects into these values before
// invoking product behavior.
package application

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/turn"
)

// Input is an event entering the application layer. The marker prevents
// transport packages from inventing ad-hoc event implementations while still
// allowing them to construct the concrete input values below.
type Input interface{ input() }

// MessageReceived is a normalized user message.
type MessageReceived struct {
	Frontend identity.FrontendID
	Chat     identity.ChatRef
	Message  InboundMessage
}

func (MessageReceived) input() {}

// CardActionReceived is a normalized Feishu card action. The Feishu adapter
// owns conversion from SDK form values to these plain values.
type CardActionReceived struct {
	Frontend identity.FrontendID
	Action   CardAction
}

func (CardActionReceived) input() {}

// CardAction contains no Feishu SDK types.
type CardAction struct {
	ActionValue Values
	FormValue   Values
	UserID      string
	ChatID      string
	MessageID   string
	Name        string
	Option      string
	InputValue  string
	Options     []string
	Checked     bool
}

// CardActionResult is the transport-neutral immediate callback response.
// Feishu adapters convert it to the platform callback envelope.
type CardActionResult struct {
	ToastType    string
	ToastContent string
}

// BackendEventReceived carries a backend-neutral event emitted by a concrete
// Codex or Claude adapter.
type BackendEventReceived struct {
	Frontend   identity.FrontendID
	SessionKey identity.SessionKey
	Event      BackendEvent
}

func (BackendEventReceived) input() {}

// BackendEvent deliberately contains semantic identifiers instead of raw RPC
// envelopes or stream-json values. Payload is adapter-normalized data owned by
// the event kind and must not contain protocol envelope types.
type BackendEvent struct {
	Kind          string
	ThreadID      string
	TurnID        string
	ResponseToken string
	RequestID     string
	Status        string
	Message       string
	// Item, Usage and Goal are typed semantic payloads. Payload remains only
	// for request-specific interaction values while those are migrated to
	// dedicated event fields.
	Item  *turn.ProtocolItem
	Usage *turn.ThreadTokenUsage
	Goal  *conversation.ThreadGoal
	// Interaction payloads are explicit so application consumers do not need
	// to type-switch an unbounded adapter value. Payload is retained as a
	// compatibility mirror for older callers and tests during migration.
	Approval        *ApprovalRequested
	UserInput       *interaction.ToolUserInputPayload
	ElicitationURL  *interaction.ElicitationURLPayload
	ElicitationForm *interaction.ElicitationFormPayload
	Rejected        *RequestRejected
	Payload         any
}

const (
	EventTurnStarted     = "turn_started"
	EventTurnCompleted   = "turn_completed"
	EventTurnError       = "turn_error"
	EventRequestResolved = "request_resolved"
)

type MessageRecalled struct {
	Frontend          identity.FrontendID
	MessageID, ChatID string
}

func (MessageRecalled) input() {}

type MessageReacted struct {
	Frontend                             identity.FrontendID
	MessageID, ChatID, UserID, EmojiType string
}

func (MessageReacted) input() {}

// RetryTimerFired carries a generation token; stale or cancelled timers cannot start work.
type RetryTimerFired struct {
	Frontend   identity.FrontendID
	SessionKey identity.SessionKey
	Sequence   uint64
}

func (RetryTimerFired) input() {}
