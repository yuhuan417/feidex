// Package serverrequest owns pending request business logic:
// approval, user input, elicitation, cancel, and backend adapter.
package serverrequest

import (
	"encoding/json"
	"strings"
	"time"

	"feidex/internal/app/pendingforms"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Payload type aliases re-exported from pendingforms.
type (
	ToolUserInputPayload    = pendingforms.ToolUserInputPayload
	ToolUserInputQuestion   = pendingforms.ToolUserInputQuestion
	ToolUserInputOption     = pendingforms.ToolUserInputOption
	ToolUserInputFormDrafts = pendingforms.FormDrafts
	ElicitationFormPayload  = pendingforms.ElicitationFormPayload
	ElicitationURLPayload   = pendingforms.ElicitationURLPayload
)

// PendingCardDelivery describes how to deliver a pending request card.
type PendingCardDelivery struct {
	RequestKey      string
	RequestIDStored string
	Backend         string
	Kind            string
	SessionKey      string
	ThreadID        string
	TurnID          string
	ItemID          string
	OwnerUserID     string
	PayloadJSON     string
	WaitingStatus   string
	LinkKind        string
	TTL             time.Duration
}

// UIWarningError is an error that should be displayed as a warning toast
// in the Feishu UI rather than logged as a server error.
type UIWarningError struct{ Message string }

func (e UIWarningError) Error() string { return e.Message }

// IsUIWarningError reports whether err is a UIWarningError.
func IsUIWarningError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(UIWarningError)
	return ok
}

// Pure helpers.

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func requestIDKey(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(raw))
}

func requestIDStored(raw json.RawMessage) string {
	return strings.TrimSpace(string(raw))
}

func requestIDRaw(key string) json.RawMessage {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	b, err := json.Marshal(key)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

func pendingRequestIDRaw(pending *state.PendingRequest) json.RawMessage {
	if pending != nil {
		if raw := strings.TrimSpace(pending.RequestIDRaw); raw != "" {
			return json.RawMessage(raw)
		}
		return requestIDRaw(pending.ID)
	}
	return nil
}

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func actionStringValue(actionValue map[string]any, key string) string {
	if actionValue == nil {
		return ""
	}
	value, _ := actionValue[key].(string)
	return strings.TrimSpace(value)
}
