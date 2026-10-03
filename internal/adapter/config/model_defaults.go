package config

import (
	scoped "feidex/internal/adapter/storage/json/scoped"
	modelapp "feidex/internal/application/modelconfig"
	fileconfig "feidex/internal/config"
	domain "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	"feidex/internal/state"
	"fmt"
	"path/filepath"
	"strings"
)

type ModelDefaultsRepository struct {
	Source Source
	Scope  *scoped.Store
}

func (r ModelDefaultsRepository) UpdateDefaults(mutate func(*modelapp.DefaultsRevision) error) error {
	r.Source.ConfigMu().Lock()
	defer r.Source.ConfigMu().Unlock()
	next := fileconfig.Clone(r.Source.Config())
	expected := r.Scope.StateStore().WorkspaceState()
	var profile *routing.BotProfile
	frontend := strings.TrimSpace(r.Scope.FrontendID())
	if frontend == "" {
		frontend = "default"
	}
	for _, candidate := range expected.Profiles {
		if candidate != nil && candidate.FrontendID == frontend {
			copy := *candidate
			profile = &copy
			break
		}
	}
	revision := modelapp.DefaultsRevision{Values: globalModelValues(next), Profile: profile}
	if err := mutate(&revision); err != nil {
		return err
	}
	if revision.Profile == nil {
		return fmt.Errorf("model defaults profile missing")
	}
	applyGlobalModelValues(next, revision.Values)
	if err := next.Normalize(filepath.Dir(r.Source.ConfigPath())); err != nil {
		return err
	}
	err := r.Scope.StateStore().CommitWorkspace(state.WorkspaceMutation{Expected: expected, Profiles: []*routing.BotProfile{revision.Profile}}, func() error {
		if r.Source.ConfigPath() == "" {
			return nil
		}
		return fileconfig.Save(r.Source.ConfigPath(), next)
	})
	if err != nil {
		return err
	}
	*r.Source.Config() = *next
	return nil
}

func globalModelValues(cfg *fileconfig.Config) domain.GlobalValues {
	return domain.GlobalValues{Model: cfg.Codex.Model, Effort: cfg.Codex.ReasoningEffort, PlanModel: cfg.Codex.PlanModel, PlanEffort: cfg.Codex.PlanReasoningEffort, ReviewModel: cfg.Codex.ReviewModel, SubagentModel: cfg.Codex.SubagentModel, SubagentEffort: cfg.Codex.SubagentReasoningEffort, ClaudeModel: cfg.Claude.Model, ClaudeEffort: cfg.Claude.Effort, ClaudeSmallModel: cfg.Claude.SmallModel, ClaudeSubagent: cfg.Claude.SubagentModel}
}
func applyGlobalModelValues(cfg *fileconfig.Config, v domain.GlobalValues) {
	cfg.Codex.Model, cfg.Codex.ReasoningEffort = v.Model, v.Effort
	cfg.Codex.PlanModel, cfg.Codex.PlanReasoningEffort = v.PlanModel, v.PlanEffort
	cfg.Codex.ReviewModel, cfg.Codex.SubagentModel, cfg.Codex.SubagentReasoningEffort = v.ReviewModel, v.SubagentModel, v.SubagentEffort
	cfg.Claude.Model, cfg.Claude.Effort, cfg.Claude.SmallModel, cfg.Claude.SubagentModel = v.ClaudeModel, v.ClaudeEffort, v.ClaudeSmallModel, v.ClaudeSubagent
}
