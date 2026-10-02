// Package pendingforms defines payload types for pending form interactions
// used by both Claude and Codex backends.
package pendingforms

import (
	"feidex/internal/domain/interaction"
	"strings"
)

const AsyncUserInputPendingKind = interaction.AsyncUserInputPendingKind

type ToolUserInputOption = interaction.ToolUserInputOption
type ToolUserInputQuestion = interaction.ToolUserInputQuestion
type ToolUserInputPayload = interaction.ToolUserInputPayload
type ElicitationFormPayload = interaction.ElicitationFormPayload
type ElicitationURLPayload = interaction.ElicitationURLPayload

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
