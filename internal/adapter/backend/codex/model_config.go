// Package codex converts Codex protocol values into application and domain values.
package codex

import (
	"strings"

	"feidex/internal/domain/modelconfig"
)

// ResumedThreadConfig records only settings acknowledged by thread/resume.
// Turn-level effort remains unknown until a local turn/start succeeds.
func ResumedThreadConfig(model string, config map[string]any) modelconfig.Snapshot {
	value := func(key string) string {
		text, _ := config[key].(string)
		return strings.TrimSpace(text)
	}
	return modelconfig.Snapshot{
		Valid: true, Backend: "codex", Model: strings.TrimSpace(model),
		ReviewModel:    value("review_model"),
		SubagentModel:  value("agents.default_subagent_model"),
		SubagentEffort: value("agents.default_subagent_reasoning_effort"),
	}
}
