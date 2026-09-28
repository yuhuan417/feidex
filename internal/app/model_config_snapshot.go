package app

import (
	"strings"

	"feidex/internal/app/modelconfig"
	"feidex/internal/config"
	"feidex/internal/state"
)

// Resolve from one config, binding and profile revision. Callers must not hold
// ConfigMu; writers of profiles/bindings use the same lock.
func modelConfigSnapshot(a *App, sess *state.Session, backend string) state.ModelConfigSnapshot {
	store := a.State() // Construct the frontend-scoped facade before ConfigMu.
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	binding := &state.AgentBinding{}
	profile := &state.BotProfile{}
	if sess == nil {
		sess = &state.Session{}
	}
	if store != nil {
		if value := store.AgentBinding(sess.BindingID); value != nil {
			binding = value
		}
		if value := store.BotProfile(); value != nil {
			profile = value
		}
	}
	cfg := a.cfg
	result := state.ModelConfigSnapshot{Valid: true, Backend: backend}
	if backend == backendClaude {
		result.Model = firstNonEmpty(sess.ModelOverride, binding.ModelOverride, profile.ClaudeModel, cfg.Claude.Model)
		result.Effort = firstNonEmpty(binding.ReasoningEffortOverride, profile.ReasoningEffort, cfg.Claude.Effort)
		result.SmallModel = firstNonEmpty(sess.SmallModelOverride, binding.SmallModelOverride, profile.ClaudeSmallModel, cfg.Claude.SmallModel)
		result.SubagentModel = firstNonEmpty(sess.SubagentModelOverride, binding.SubagentModelOverride, profile.ClaudeSubagentModel, cfg.Claude.SubagentModel, result.Model)
		return result
	}
	result.Model = firstNonEmpty(sess.ModelOverride, binding.ModelOverride, profile.Model, cfg.Codex.Model)
	result.Effort = firstNonEmpty(binding.ReasoningEffortOverride, profile.ReasoningEffort, cfg.Codex.ReasoningEffort)
	result.PlanModel = firstNonEmpty(sess.PlanModelOverride, binding.PlanModelOverride, profile.PlanModel, cfg.Codex.PlanModel, result.Model)
	result.PlanEffort = firstNonEmpty(sess.PlanReasoningEffortOverride, binding.PlanReasoningEffortOverride, profile.PlanReasoningEffort, cfg.Codex.PlanReasoningEffort)
	result.ReviewModel = firstNonEmpty(sess.ReviewModelOverride, binding.ReviewModelOverride, profile.ReviewModel, cfg.Codex.ReviewModel, result.Model)
	result.SubagentModel = firstNonEmpty(sess.SubagentModelOverride, binding.SubagentModelOverride, profile.SubagentModel, cfg.Codex.SubagentModel, result.Model)
	result.SubagentEffort = firstNonEmpty(sess.SubagentReasoningEffortOverride, binding.SubagentReasoningEffortOverride, profile.SubagentReasoningEffort, cfg.Codex.SubagentReasoningEffort, result.Effort)
	if sess.ActiveThreadCollaborationMode != nil {
		result.CollaborationMode = sess.ActiveThreadCollaborationMode.Mode
		result.PlanEffort = firstNonEmpty(result.PlanEffort, sess.ActiveThreadCollaborationMode.PresetReasoningEffort)
		result.Model = firstNonEmpty(result.Model, sess.ActiveThreadCollaborationMode.Model)
		result.PlanModel = firstNonEmpty(result.PlanModel, result.Model)
	}
	return result
}

func (a submissionAppAdapter) SubmissionQueueResolveModelConfig(sess *state.Session, sub *state.Submission) state.ModelConfigSnapshot {
	if sess != nil && sub != nil && sub.BindingID != "" {
		cp := *sess
		cp.BindingID = sub.BindingID
		sess = &cp
	}
	return modelConfigSnapshot(a.app, sess, configuredBackend(a.app))
}

func modelConfigStatus(a *App, sessionKey string) string {
	backend := configuredBackend(a)
	notice := modelconfig.TurnApplyNotice
	if backend == backendCodex {
		notice += "\n" + modelconfig.CodexAuxiliaryApplyNotice
	}
	if backend == backendClaude {
		notice += "\n" + modelconfig.ClaudeAuxiliaryApplyNotice
	}
	if a.State() == nil {
		return notice
	}
	sess := a.State().Session(normalizeSessionKey(a, sessionKey))
	if sess == nil {
		return notice
	}
	if sess.ModelConfigError != "" {
		notice += "\n配置应用失败/待生效：" + sess.ModelConfigError
	}
	applied := sess.AppliedModelConfig
	if applied.Valid && applied.Backend == backend && strings.TrimSpace(sess.ActiveThreadID) != "" {
		notice += "\n最近已应用模型：`" + firstNonEmpty(applied.Model, "默认") + "`；推理强度：`" + firstNonEmpty(applied.Effort, "默认") + "`。"
		if modelConfigSnapshot(a, sess, backend) != applied {
			notice += "\n已保存配置与当前应用值不同，待对应边界生效。"
		}
	} else {
		notice += "\n当前会话尚无已确认的配置应用记录。"
	}
	return notice
}

func modelConfigReadCopy(a *App) *config.Config {
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return config.Clone(a.cfg)
}
