package app

import (
	"encoding/json"
	"strings"

	"feidex/internal/state"
)

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

func requestIDStored(raw json.RawMessage) string {
	return strings.TrimSpace(string(raw))
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
