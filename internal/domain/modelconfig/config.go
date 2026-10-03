// Package modelconfig contains the backend-independent model configuration
// value objects and scope resolution rules.
package modelconfig

import "strings"

const (
	BackendCodex  = "codex"
	BackendClaude = "claude"
)

// Snapshot is the model configuration captured at a safe turn boundary.
// It is deliberately independent of persistence and backend protocol types.
type Snapshot struct {
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

// Sources contains the values available at each configuration scope. The
// resolver applies the same precedence for both card rendering and turn
// startup so a chat cannot accidentally inherit another scope's model.
type Sources struct {
	Session SessionValues
	Binding ScopeValues
	Profile ProfileValues
	Global  GlobalValues
	Active  *ActiveCollaboration
}

// SessionValues contains session-scoped overrides.
type SessionValues struct {
	Model          string
	PlanModel      string
	PlanEffort     string
	ReviewModel    string
	SubagentModel  string
	SubagentEffort string
	SmallModel     string
}

// ScopeValues contains group/binding-scoped overrides.
type ScopeValues struct {
	Model          string
	Effort         string
	PlanModel      string
	PlanEffort     string
	ReviewModel    string
	SubagentModel  string
	SubagentEffort string
	SmallModel     string
}

// ProfileValues contains frontend/BotProfile-scoped values.
type ProfileValues struct {
	Model            string
	ClaudeModel      string
	Effort           string
	PlanModel        string
	PlanEffort       string
	ReviewModel      string
	SubagentModel    string
	SubagentEffort   string
	ClaudeSubagent   string
	SmallModel       string
	ClaudeSmallModel string
}

// GlobalValues contains backend-global configuration.
type GlobalValues struct {
	Model            string
	Effort           string
	PlanModel        string
	PlanEffort       string
	ReviewModel      string
	SubagentModel    string
	SubagentEffort   string
	ClaudeModel      string
	ClaudeEffort     string
	ClaudeSmallModel string
	ClaudeSubagent   string
	SmallModel       string
}

// ActiveCollaboration describes a currently bound Codex collaboration mode.
type ActiveCollaboration struct {
	Mode         string
	Model        string
	PresetEffort string
}

// Resolve applies scope precedence and returns a detached snapshot.
func Resolve(backend string, sources Sources) Snapshot {
	backend = strings.TrimSpace(backend)
	result := Snapshot{Valid: true, Backend: backend}
	if backend == BackendClaude {
		result.Model = first(sources.Session.Model, sources.Binding.Model, sources.Profile.ClaudeModel, sources.Global.ClaudeModel)
		result.Effort = first(sources.Binding.Effort, sources.Profile.Effort, sources.Global.ClaudeEffort)
		result.SmallModel = first(sources.Session.SmallModel, sources.Binding.SmallModel, sources.Profile.ClaudeSmallModel, sources.Global.ClaudeSmallModel)
		result.SubagentModel = first(sources.Session.SubagentModel, sources.Binding.SubagentModel, sources.Profile.ClaudeSubagent, sources.Global.ClaudeSubagent, result.Model)
		return result
	}

	result.Model = first(sources.Session.Model, sources.Binding.Model, sources.Profile.Model, sources.Global.Model)
	result.Effort = first(sources.Binding.Effort, sources.Profile.Effort, sources.Global.Effort)
	result.PlanModel = first(sources.Session.PlanModel, sources.Binding.PlanModel, sources.Profile.PlanModel, sources.Global.PlanModel, result.Model)
	result.PlanEffort = first(sources.Session.PlanEffort, sources.Binding.PlanEffort, sources.Profile.PlanEffort, sources.Global.PlanEffort)
	result.ReviewModel = first(sources.Session.ReviewModel, sources.Binding.ReviewModel, sources.Profile.ReviewModel, sources.Global.ReviewModel, result.Model)
	result.SubagentModel = first(sources.Session.SubagentModel, sources.Binding.SubagentModel, sources.Profile.SubagentModel, sources.Global.SubagentModel, result.Model)
	result.SubagentEffort = first(sources.Session.SubagentEffort, sources.Binding.SubagentEffort, sources.Profile.SubagentEffort, sources.Global.SubagentEffort, result.Effort)
	if active := sources.Active; active != nil {
		result.CollaborationMode = strings.TrimSpace(active.Mode)
		result.PlanEffort = first(result.PlanEffort, active.PresetEffort)
		result.Model = first(result.Model, active.Model)
		result.PlanModel = first(result.PlanModel, result.Model)
	}
	return result
}

// TurnSettings selects the model and effort that a snapshot applies to.
func TurnSettings(snapshot Snapshot) (model, effort string) {
	if snapshot.Backend == BackendCodex && snapshot.CollaborationMode == "plan" {
		return first(snapshot.PlanModel, snapshot.Model), strings.TrimSpace(snapshot.PlanEffort)
	}
	return strings.TrimSpace(snapshot.Model), strings.TrimSpace(snapshot.Effort)
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func ResolveAuxiliary(s Sources) GlobalValues {
	return GlobalValues{
		PlanModel:        first(s.Session.PlanModel, s.Binding.PlanModel, s.Profile.PlanModel, s.Global.PlanModel),
		PlanEffort:       first(s.Session.PlanEffort, s.Binding.PlanEffort, s.Profile.PlanEffort, s.Global.PlanEffort),
		ReviewModel:      first(s.Session.ReviewModel, s.Binding.ReviewModel, s.Profile.ReviewModel, s.Global.ReviewModel),
		SubagentModel:    first(s.Session.SubagentModel, s.Binding.SubagentModel, s.Profile.SubagentModel, s.Global.SubagentModel),
		SubagentEffort:   first(s.Session.SubagentEffort, s.Binding.SubagentEffort, s.Profile.SubagentEffort, s.Global.SubagentEffort),
		ClaudeSubagent:   first(s.Session.SubagentModel, s.Binding.SubagentModel, s.Profile.ClaudeSubagent, s.Global.ClaudeSubagent),
		ClaudeSmallModel: first(s.Session.SmallModel, s.Binding.SmallModel, s.Profile.ClaudeSmallModel, s.Global.ClaudeSmallModel),
	}
}
