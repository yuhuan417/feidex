package state

import "strings"

// ModelConfigSnapshot is captured at a locally initiated turn boundary. It is
// a value type so submissions never share mutable configuration with editors.
type ModelConfigSnapshot struct {
	Valid             bool   `json:"valid,omitempty"`
	Backend           string `json:"backend,omitempty"`
	Model             string `json:"model,omitempty"`
	Effort            string `json:"effort,omitempty"`
	PlanModel         string `json:"plan_model,omitempty"`
	PlanEffort        string `json:"plan_effort,omitempty"`
	ReviewModel       string `json:"review_model,omitempty"`
	SubagentModel     string `json:"subagent_model,omitempty"`
	SubagentEffort    string `json:"subagent_effort,omitempty"`
	SmallModel        string `json:"small_model,omitempty"`
	CollaborationMode string `json:"collaboration_mode,omitempty"`
}

// CodexResumedThreadConfig records only settings acknowledged by thread/resume.
// Turn-level effort remains unknown until a local turn/start succeeds.
func CodexResumedThreadConfig(model string, config map[string]any) ModelConfigSnapshot {
	value := func(key string) string {
		text, _ := config[key].(string)
		return strings.TrimSpace(text)
	}
	return ModelConfigSnapshot{
		Valid: true, Backend: "codex", Model: strings.TrimSpace(model),
		ReviewModel:    value("review_model"),
		SubagentModel:  value("agents.default_subagent_model"),
		SubagentEffort: value("agents.default_subagent_reasoning_effort"),
	}
}
