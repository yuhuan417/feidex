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

// Origin names the configuration tier a resolved setting value came from.
type Origin string

const (
	OriginSession Origin = "session"
	OriginBinding Origin = "binding"
	OriginProfile Origin = "profile"
	OriginGlobal  Origin = "global"
	// OriginDerived marks a value synthesized from another resolved value,
	// such as a plan model following the main model.
	OriginDerived Origin = "derived"
	// OriginNone marks a setting with no configured value in any tier.
	OriginNone Origin = ""
)

// Origins records the origin tier of each resolved snapshot field.
type Origins struct {
	Model, Effort, PlanModel, PlanEffort, ReviewModel, SubagentModel, SubagentEffort, SmallModel Origin
}

// Resolve applies scope precedence and returns a detached snapshot.
func Resolve(backend string, sources Sources) Snapshot {
	snapshot, _ := ResolveTraced(backend, sources)
	return snapshot
}

// ResolveTraced applies scope precedence like Resolve and additionally reports
// the tier each value came from, so cards can annotate effective values with a
// consistent source description instead of hand-rolled fallback chains.
func ResolveTraced(backend string, sources Sources) (Snapshot, Origins) {
	backend = strings.TrimSpace(backend)
	origins := Origins{}
	result := Snapshot{Valid: true, Backend: backend}
	if backend == BackendClaude {
		result.Model, origins.Model = firstTiered(
			tiered{OriginSession, sources.Session.Model},
			tiered{OriginBinding, sources.Binding.Model},
			tiered{OriginProfile, sources.Profile.ClaudeModel},
			tiered{OriginGlobal, sources.Global.ClaudeModel},
		)
		result.Effort, origins.Effort = firstTiered(
			tiered{OriginBinding, sources.Binding.Effort},
			tiered{OriginProfile, sources.Profile.Effort},
			tiered{OriginGlobal, sources.Global.ClaudeEffort},
		)
		result.SmallModel, origins.SmallModel = firstTiered(
			tiered{OriginSession, sources.Session.SmallModel},
			tiered{OriginBinding, sources.Binding.SmallModel},
			tiered{OriginProfile, sources.Profile.ClaudeSmallModel},
			tiered{OriginGlobal, sources.Global.ClaudeSmallModel},
		)
		result.SubagentModel, origins.SubagentModel = firstTiered(
			tiered{OriginSession, sources.Session.SubagentModel},
			tiered{OriginBinding, sources.Binding.SubagentModel},
			tiered{OriginProfile, sources.Profile.ClaudeSubagent},
			tiered{OriginGlobal, sources.Global.ClaudeSubagent},
		)
		if result.SubagentModel == "" && result.Model != "" {
			result.SubagentModel, origins.SubagentModel = result.Model, OriginDerived
		}
		return result, origins
	}

	result.Model, origins.Model = firstTiered(
		tiered{OriginSession, sources.Session.Model},
		tiered{OriginBinding, sources.Binding.Model},
		tiered{OriginProfile, sources.Profile.Model},
		tiered{OriginGlobal, sources.Global.Model},
	)
	result.Effort, origins.Effort = firstTiered(
		tiered{OriginBinding, sources.Binding.Effort},
		tiered{OriginProfile, sources.Profile.Effort},
		tiered{OriginGlobal, sources.Global.Effort},
	)
	result.PlanModel, origins.PlanModel = firstTiered(
		tiered{OriginSession, sources.Session.PlanModel},
		tiered{OriginBinding, sources.Binding.PlanModel},
		tiered{OriginProfile, sources.Profile.PlanModel},
		tiered{OriginGlobal, sources.Global.PlanModel},
	)
	result.PlanEffort, origins.PlanEffort = firstTiered(
		tiered{OriginSession, sources.Session.PlanEffort},
		tiered{OriginBinding, sources.Binding.PlanEffort},
		tiered{OriginProfile, sources.Profile.PlanEffort},
		tiered{OriginGlobal, sources.Global.PlanEffort},
	)
	result.ReviewModel, origins.ReviewModel = firstTiered(
		tiered{OriginSession, sources.Session.ReviewModel},
		tiered{OriginBinding, sources.Binding.ReviewModel},
		tiered{OriginProfile, sources.Profile.ReviewModel},
		tiered{OriginGlobal, sources.Global.ReviewModel},
	)
	result.SubagentModel, origins.SubagentModel = firstTiered(
		tiered{OriginSession, sources.Session.SubagentModel},
		tiered{OriginBinding, sources.Binding.SubagentModel},
		tiered{OriginProfile, sources.Profile.SubagentModel},
		tiered{OriginGlobal, sources.Global.SubagentModel},
	)
	result.SubagentEffort, origins.SubagentEffort = firstTiered(
		tiered{OriginSession, sources.Session.SubagentEffort},
		tiered{OriginBinding, sources.Binding.SubagentEffort},
		tiered{OriginProfile, sources.Profile.SubagentEffort},
		tiered{OriginGlobal, sources.Global.SubagentEffort},
	)
	if result.PlanModel == "" && result.Model != "" {
		result.PlanModel, origins.PlanModel = result.Model, OriginDerived
	}
	if result.ReviewModel == "" && result.Model != "" {
		result.ReviewModel, origins.ReviewModel = result.Model, OriginDerived
	}
	if result.SubagentModel == "" && result.Model != "" {
		result.SubagentModel, origins.SubagentModel = result.Model, OriginDerived
	}
	if result.SubagentEffort == "" && result.Effort != "" {
		result.SubagentEffort, origins.SubagentEffort = result.Effort, OriginDerived
	}
	if active := sources.Active; active != nil {
		result.CollaborationMode = strings.TrimSpace(active.Mode)
		if result.PlanEffort == "" && strings.TrimSpace(active.PresetEffort) != "" {
			result.PlanEffort, origins.PlanEffort = strings.TrimSpace(active.PresetEffort), OriginDerived
		}
		if result.Model == "" && strings.TrimSpace(active.Model) != "" {
			result.Model, origins.Model = strings.TrimSpace(active.Model), OriginDerived
		}
		if result.PlanModel == "" && result.Model != "" {
			result.PlanModel, origins.PlanModel = result.Model, OriginDerived
		}
	}
	return result, origins
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

type tiered struct {
	origin Origin
	value  string
}

func firstTiered(values ...tiered) (string, Origin) {
	for _, item := range values {
		if value := strings.TrimSpace(item.value); value != "" {
			return value, item.origin
		}
	}
	return "", OriginNone
}

// TurnSettings selects the model and effort that a snapshot applies to.
func TurnSettings(snapshot Snapshot) (model, effort string) {
	if snapshot.Backend == BackendCodex && snapshot.CollaborationMode == "plan" {
		return first(snapshot.PlanModel, snapshot.Model), strings.TrimSpace(snapshot.PlanEffort)
	}
	return strings.TrimSpace(snapshot.Model), strings.TrimSpace(snapshot.Effort)
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
