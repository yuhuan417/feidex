package config

import (
	"strings"
	"sync"

	"feidex/internal/application/modelconfig"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
)

type ModelScopes interface {
	AgentBinding(string) *routing.AgentBinding
	BotProfile() *routing.BotProfile
}

// ModelSourceRepository uses the same revision lock as config/profile/binding
// writers, so a turn sees one configuration revision across all three scopes.
type ModelSourceRepository struct {
	Config *fileconfig.Config
	Mutex  *sync.RWMutex
	Scopes ModelScopes
}

func (r ModelSourceRepository) ModelSourceRevision(sess *conversation.Session) modelconfig.SourceRevision {
	if r.Mutex != nil {
		r.Mutex.RLock()
		defer r.Mutex.RUnlock()
	}
	result := modelconfig.SourceRevision{Session: conversation.CloneSession(sess)}
	if r.Scopes != nil {
		if sess != nil && strings.TrimSpace(sess.BindingID) != "" {
			result.Binding = r.Scopes.AgentBinding(sess.BindingID)
		}
		result.Profile = r.Scopes.BotProfile()
	}
	if cfg := r.Config; cfg != nil {
		result.Global = domain.GlobalValues{Model: cfg.Codex.Model, Effort: cfg.Codex.ReasoningEffort,
			PlanModel: cfg.Codex.PlanModel, PlanEffort: cfg.Codex.PlanReasoningEffort,
			ReviewModel: cfg.Codex.ReviewModel, SubagentModel: cfg.Codex.SubagentModel,
			SubagentEffort: cfg.Codex.SubagentReasoningEffort,
			ClaudeModel:    cfg.Claude.Model, ClaudeEffort: cfg.Claude.Effort,
			ClaudeSmallModel: cfg.Claude.SmallModel, ClaudeSubagent: cfg.Claude.SubagentModel}
	}
	return result
}
