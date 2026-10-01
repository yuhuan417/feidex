package app

import (
	"strings"

	"feidex/internal/app/apputil"
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
		result.Model = apputil.FirstNonEmpty(sess.ModelOverride, binding.ModelOverride, profile.ClaudeModel, cfg.Claude.Model)
		result.Effort = apputil.FirstNonEmpty(binding.ReasoningEffortOverride, profile.ReasoningEffort, cfg.Claude.Effort)
		result.SmallModel = apputil.FirstNonEmpty(sess.SmallModelOverride, binding.SmallModelOverride, profile.ClaudeSmallModel, cfg.Claude.SmallModel)
		result.SubagentModel = apputil.FirstNonEmpty(sess.SubagentModelOverride, binding.SubagentModelOverride, profile.ClaudeSubagentModel, cfg.Claude.SubagentModel, result.Model)
		return result
	}
	result.Model = apputil.FirstNonEmpty(sess.ModelOverride, binding.ModelOverride, profile.Model, cfg.Codex.Model)
	result.Effort = apputil.FirstNonEmpty(binding.ReasoningEffortOverride, profile.ReasoningEffort, cfg.Codex.ReasoningEffort)
	result.PlanModel = apputil.FirstNonEmpty(sess.PlanModelOverride, binding.PlanModelOverride, profile.PlanModel, cfg.Codex.PlanModel, result.Model)
	result.PlanEffort = apputil.FirstNonEmpty(sess.PlanReasoningEffortOverride, binding.PlanReasoningEffortOverride, profile.PlanReasoningEffort, cfg.Codex.PlanReasoningEffort)
	result.ReviewModel = apputil.FirstNonEmpty(sess.ReviewModelOverride, binding.ReviewModelOverride, profile.ReviewModel, cfg.Codex.ReviewModel, result.Model)
	result.SubagentModel = apputil.FirstNonEmpty(sess.SubagentModelOverride, binding.SubagentModelOverride, profile.SubagentModel, cfg.Codex.SubagentModel, result.Model)
	result.SubagentEffort = apputil.FirstNonEmpty(sess.SubagentReasoningEffortOverride, binding.SubagentReasoningEffortOverride, profile.SubagentReasoningEffort, cfg.Codex.SubagentReasoningEffort, result.Effort)
	if sess.ActiveThreadCollaborationMode != nil {
		result.CollaborationMode = sess.ActiveThreadCollaborationMode.Mode
		result.PlanEffort = apputil.FirstNonEmpty(result.PlanEffort, sess.ActiveThreadCollaborationMode.PresetReasoningEffort)
		result.Model = apputil.FirstNonEmpty(result.Model, sess.ActiveThreadCollaborationMode.Model)
		result.PlanModel = apputil.FirstNonEmpty(result.PlanModel, result.Model)
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
	desired := modelConfigSnapshot(a, sess, backend)
	nextModel, nextEffort := modelConfigTurnSettings(desired)
	notice += "\n下一轮本地启动模型：`" + apputil.FirstNonEmpty(nextModel, "默认") + "`；推理强度：`" + apputil.FirstNonEmpty(nextEffort, "默认") + "`。"
	if sess.ModelConfigError != "" {
		notice += "\n配置应用失败/待生效：" + sess.ModelConfigError
	}
	applied := sess.AppliedModelConfig
	if applied.Valid && applied.Backend == backend && strings.TrimSpace(sess.ActiveThreadID) != "" {
		appliedModel, appliedEffort := modelConfigTurnSettings(applied)
		notice += "\n最近已应用模型：`" + apputil.FirstNonEmpty(appliedModel, "默认") + "`；推理强度：`" + apputil.FirstNonEmpty(appliedEffort, "默认") + "`。"
		if desired != applied {
			notice += "\n已保存配置与当前应用值不同，待对应边界生效。"
		}
	} else {
		notice += "\n当前会话尚无已确认的配置应用记录。"
	}
	return notice
}

func modelConfigTurnSettings(snapshot state.ModelConfigSnapshot) (string, string) {
	if snapshot.Backend == backendCodex && snapshot.CollaborationMode == "plan" {
		return apputil.FirstNonEmpty(snapshot.PlanModel, snapshot.Model), snapshot.PlanEffort
	}
	return snapshot.Model, snapshot.Effort
}

func modelConfigReadCopy(a *App) *config.Config {
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return config.Clone(a.cfg)
}
