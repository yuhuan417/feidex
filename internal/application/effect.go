package application

import (
	"crypto/sha256"
	"encoding/hex"
	"feidex/internal/application/backendops"
	"feidex/internal/application/presentation"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"strings"
)

// Effect is a side effect produced after a domain/application transition.
// Runtime executes effects after state changes have been persisted.
type Effect interface{ effect() }

// SaveState persists a conversation snapshot before further effects run.
type SaveState struct {
	Frontend identity.FrontendID
	Session  *conversation.Session
	// IdempotencyKey is optional because a state snapshot may legitimately be
	// saved more than once while an actor is settling.
	IdempotencyKey string
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
	IdempotencyKey string
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
	IdempotencyKey string
}

func (SendCard) effect() {}

// PatchCard is a semantic card update. The application carries only the
// detached presentation value; the Feishu adapter chooses the wire payload.
type PatchCard struct {
	Frontend       identity.FrontendID
	MessageID      string
	View           presentation.CardView
	IdempotencyKey string
}

func (PatchCard) effect() {}

// StartTurn starts local work using settings captured at the startup boundary.
type StartTurn struct {
	Frontend       identity.FrontendID
	SessionKey     identity.SessionKey
	Request        backendops.StartTurnRequest
	IdempotencyKey string
}

func (StartTurn) effect() {}

// ResolveBackendRequest sends a response to the backend. It does not mark a
// Codex interaction resolved; serverRequest/resolved remains authoritative.
type ResolveBackendRequest struct {
	Frontend       identity.FrontendID
	Backend        string
	Response       backendops.Response
	IdempotencyKey string
}

func (ResolveBackendRequest) effect() {}

// SteerTurn targets the already-active turn; it never applies new model settings.
type SteerTurn struct {
	Frontend                                   identity.FrontendID
	SessionKey, ThreadID, ExpectedTurnID, Text string
	IdempotencyKey                             string
}

func (SteerTurn) effect() {}

type EnqueueInput struct {
	Frontend            identity.FrontendID
	SessionKey          string
	BindOnlyCurrentRoot bool
	Message             InboundMessage
	IdempotencyKey      string
}

func (EnqueueInput) effect() {}

// RefreshGroupStatus schedules display refresh after a persisted binding change.
type RefreshGroupStatus struct {
	Frontend       identity.FrontendID
	ChatID, Reason string
	IdempotencyKey string
}

func (RefreshGroupStatus) effect() {}

// EffectIdentity returns a stable key for an effect when retrying it would
// repeat an external operation. Explicit keys always win; the safe defaults
// use identities already owned by the domain (submission, message, turn, or
// backend request token). Effects without a stable identity return empty and
// are executed on every attempt.
func EffectIdentity(effect Effect) string {
	if effect == nil {
		return ""
	}
	var explicit string
	switch e := effect.(type) {
	case SaveState:
		explicit = e.IdempotencyKey
	case SendMessage:
		if strings.TrimSpace(e.IdempotencyKey) != "" {
			return e.IdempotencyKey
		}
		if strings.TrimSpace(e.ReplyMessageID) != "" {
			return StableEffectKey("reply-message", string(e.Frontend), e.ReplyMessageID, e.Text, boolString(e.InThread))
		}
	case SendCard:
		explicit = e.IdempotencyKey
	case PatchCard:
		explicit = e.IdempotencyKey
	case StartTurn:
		explicit = e.IdempotencyKey
		if explicit == "" && e.Request.Submission != nil && strings.TrimSpace(e.Request.Submission.ID) != "" {
			return StableEffectKey("start-turn", string(e.Frontend), string(e.SessionKey), e.Request.Submission.ID)
		}
	case ResolveBackendRequest:
		explicit = e.IdempotencyKey
		if explicit == "" && len(e.Response.Token) > 0 {
			return StableEffectKey("resolve-request", string(e.Frontend), e.Backend, string(e.Response.Token))
		}
	case SteerTurn:
		explicit = e.IdempotencyKey
		if explicit == "" && strings.TrimSpace(e.ExpectedTurnID) != "" {
			return StableEffectKey("steer-turn", string(e.Frontend), e.ThreadID, e.ExpectedTurnID, e.Text)
		}
	case EnqueueInput:
		explicit = e.IdempotencyKey
		if explicit == "" && strings.TrimSpace(e.Message.MessageID) != "" {
			return StableEffectKey("enqueue-input", string(e.Frontend), e.SessionKey, e.Message.MessageID)
		}
	case RefreshGroupStatus:
		explicit = e.IdempotencyKey
	}
	return strings.TrimSpace(explicit)
}

// StableEffectKey hashes a small tuple into an opaque key suitable for logs,
// persistence, and in-memory deduplication. Callers should include the
// frontend and the domain identity in the tuple.
func StableEffectKey(kind string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(kind)))
	for _, part := range parts {
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	return strings.TrimSpace(kind) + ":" + hex.EncodeToString(h.Sum(nil))
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
