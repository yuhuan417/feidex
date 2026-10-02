package codex

import (
	"encoding/json"
	"strings"
)

// RequestIDKey normalizes the JSON-RPC request id used by Codex server
// requests and notifications. Numeric ids retain their raw representation.
func RequestIDKey(raw json.RawMessage) string {
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
