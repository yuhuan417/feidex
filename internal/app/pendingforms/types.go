// Package pendingforms defines payload types for pending form interactions
// used by both Claude and Codex backends.
package pendingforms

import "strings"

// ToolUserInputOption represents a single option in a user input question.
type ToolUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ToolUserInputQuestion represents a question requiring user input.
type ToolUserInputQuestion struct {
	Header      string                `json:"header"`
	ID          string                `json:"id"`
	Question    string                `json:"question"`
	IsOther     bool                  `json:"isOther"`
	IsSecret    bool                  `json:"isSecret"`
	MultiSelect bool                  `json:"multiSelect,omitempty"`
	Options     []ToolUserInputOption `json:"options"`
}

// ToolUserInputPayload is the payload for tool-requested user input forms.
type ToolUserInputPayload struct {
	ThreadID  string                  `json:"threadId"`
	TurnID    string                  `json:"turnId"`
	ItemID    string                  `json:"itemId"`
	Questions []ToolUserInputQuestion `json:"questions"`
}

// ElicitationFormPayload is the payload for MCP elicitation form requests.
type ElicitationFormPayload struct {
	ServerName string         `json:"serverName"`
	ThreadID   string         `json:"threadId"`
	TurnID     string         `json:"turnId,omitempty"`
	Message    string         `json:"message"`
	Schema     map[string]any `json:"requestedSchema"`
}

// ElicitationURLPayload is the payload for MCP elicitation URL requests.
type ElicitationURLPayload struct {
	ServerName    string `json:"serverName"`
	ThreadID      string `json:"threadId"`
	TurnID        string `json:"turnId,omitempty"`
	ElicitationID string `json:"elicitationId"`
	Message       string `json:"message"`
	URL           string `json:"url"`
}

// ParseStructuredLines parses "key: value" lines into a map.
func ParseStructuredLines(text string) map[string]string {
	lines := strings.Split(text, "\n")
	out := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}
