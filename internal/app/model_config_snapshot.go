package app

import (
	"feidex/internal/app/apputil"
	"feidex/internal/app/modelconfig"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/config"
	domainmodelconfig "feidex/internal/domain/modelconfig"
	"feidex/internal/state"
)

// Resolve from one config, binding and profile revision. Callers must not hold
// ConfigMu; writers of profiles/bindings use the same lock.
func modelConfigSnapshot(a *App, sess *state.Session, backend string) domainmodelconfig.Snapshot {
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
	sources := domainmodelconfig.Sources{
		Session: domainmodelconfig.SessionValues{
			Model: sess.ModelOverride, PlanModel: sess.PlanModelOverride,
			PlanEffort: sess.PlanReasoningEffortOverride, ReviewModel: sess.ReviewModelOverride,
			SubagentModel: sess.SubagentModelOverride, SubagentEffort: sess.SubagentReasoningEffortOverride,
			SmallModel: sess.SmallModelOverride,
		},
		Binding: domainmodelconfig.ScopeValues{
			Model: binding.ModelOverride, Effort: binding.ReasoningEffortOverride,
			PlanModel: binding.PlanModelOverride, PlanEffort: binding.PlanReasoningEffortOverride,
			ReviewModel: binding.ReviewModelOverride, SubagentModel: binding.SubagentModelOverride,
			SubagentEffort: binding.SubagentReasoningEffortOverride, SmallModel: binding.SmallModelOverride,
		},
		Profile: domainmodelconfig.ProfileValues{
			Model: profile.Model, ClaudeModel: profile.ClaudeModel, Effort: profile.ReasoningEffort,
			PlanModel: profile.PlanModel, PlanEffort: profile.PlanReasoningEffort,
			ReviewModel: profile.ReviewModel, SubagentModel: profile.SubagentModel,
			SubagentEffort: profile.SubagentReasoningEffort, ClaudeSubagent: profile.ClaudeSubagentModel,
			ClaudeSmallModel: profile.ClaudeSmallModel,
		},
		Global: domainmodelconfig.GlobalValues{
			Model: cfg.Codex.Model, Effort: cfg.Codex.ReasoningEffort,
			PlanModel: cfg.Codex.PlanModel, PlanEffort: cfg.Codex.PlanReasoningEffort,
			ReviewModel: cfg.Codex.ReviewModel, SubagentModel: cfg.Codex.SubagentModel,
			SubagentEffort: cfg.Codex.SubagentReasoningEffort,
			ClaudeModel:    cfg.Claude.Model, ClaudeEffort: cfg.Claude.Effort,
			ClaudeSmallModel: cfg.Claude.SmallModel, ClaudeSubagent: cfg.Claude.SubagentModel,
		},
	}
	if sess.ActiveThreadCollaborationMode != nil {
		sources.Active = &domainmodelconfig.ActiveCollaboration{
			Mode:         sess.ActiveThreadCollaborationMode.Mode,
			Model:        sess.ActiveThreadCollaborationMode.Model,
			PresetEffort: sess.ActiveThreadCollaborationMode.PresetReasoningEffort,
		}
	}
	return domainmodelconfig.Resolve(backend, sources)
}

func (a submissionAppAdapter) SubmissionQueueResolveModelConfig(sess *state.Session, sub *state.Submission) domainmodelconfig.Snapshot {
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
	status := applicationmodelconfig.SessionStatus(backend, sess.ActiveThreadID, desired, sess.AppliedModelConfig, sess.ModelConfigError)
	notice += "\n下一轮本地启动模型：`" + apputil.FirstNonEmpty(status.NextModel, "默认") + "`；推理强度：`" + apputil.FirstNonEmpty(status.NextEffort, "默认") + "`。"
	if status.Error != "" {
		notice += "\n配置应用失败/待生效：" + status.Error
	}
	if status.HasApplied {
		notice += "\n最近已应用模型：`" + apputil.FirstNonEmpty(status.AppliedModel, "默认") + "`；推理强度：`" + apputil.FirstNonEmpty(status.AppliedEffort, "默认") + "`。"
		if status.Pending {
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
