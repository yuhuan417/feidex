package codexrpc

import (
	"bytes"
	"encoding/json"
	"strings"
)

type ThreadReadResult struct {
	Thread ThreadReadThread `json:"thread"`
}

type ThreadReadThread struct {
	ID      string           `json:"id"`
	Name    *string          `json:"name"`
	Preview string           `json:"preview"`
	Cwd     string           `json:"cwd"`
	Turns   []ThreadReadTurn `json:"turns"`
}

type ThreadReadTurn struct {
	ID     string               `json:"id"`
	Status string               `json:"status"`
	Error  *ThreadReadTurnError `json:"error"`
	Items  []ThreadReadItem     `json:"items"`
}

type ThreadReadTurnError struct {
	Message           string          `json:"message"`
	AdditionalDetails *string         `json:"additionalDetails"`
	CodexErrorInfo    json.RawMessage `json:"codexErrorInfo"`
}

// DisplayText retains diagnostic fields supplied alongside the error message.
func (e *ThreadReadTurnError) DisplayText() string {
	if e == nil {
		return ""
	}
	parts := []string{}
	if message := strings.TrimSpace(e.Message); message != "" {
		parts = append(parts, message)
	}
	var info bytes.Buffer
	if json.Compact(&info, e.CodexErrorInfo) == nil && info.String() != "null" {
		parts = append(parts, "错误信息: "+info.String())
	}
	if e.AdditionalDetails != nil {
		if details := strings.TrimSpace(*e.AdditionalDetails); details != "" {
			parts = append(parts, details)
		}
	}
	return strings.Join(parts, "\n")
}

type ThreadReadItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Text             string          `json:"text"`
	Phase            *string         `json:"phase"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	Status           *string         `json:"status"`
	Summary          []string        `json:"summary"`
	Content          json.RawMessage `json:"content"`
}

type ThreadReadUserInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url"`
	Path string `json:"path"`
	Name string `json:"name"`
}
