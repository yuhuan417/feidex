// Package application contains transport-independent use-case contracts.
// Concrete adapters translate their protocol objects into these values before
// invoking product behavior.
package application

import "feidex/internal/domain/identity"

// Input is an event entering the application layer. The marker prevents
// transport packages from inventing ad-hoc event implementations while still
// allowing them to construct the concrete input values below.
type Input interface{ input() }

// MessageReceived is a normalized user message.
type MessageReceived struct {
	Frontend identity.FrontendID
	Chat     identity.ChatRef
	Message  Message
}

func (MessageReceived) input() {}

// Message contains only transport-independent fields needed by use cases.
type Message struct {
	ID                   string
	UserID               string
	Text                 string
	RootMessageID        string
	ParentMessageID      string
	MentionedOpenIDs     []string
	MentionedAll         bool
	CreatedAtUnix        int64
	ExpandedMergeForward bool
}

// CardActionReceived is a normalized Feishu card action. The Feishu adapter
// owns conversion from SDK form values to these plain values.
type CardActionReceived struct {
	Frontend identity.FrontendID
	Action   CardAction
}

func (CardActionReceived) input() {}

// CardAction contains no Feishu SDK types.
type CardAction struct {
	ID         string
	Name       string
	MessageID  string
	Chat       identity.ChatRef
	UserID     string
	Values     map[string]string
	FormValues map[string]string
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
	Kind      string
	ThreadID  string
	TurnID    string
	RequestID string
	Payload   any
}
