package app

import (
	"strings"

	"feidex/internal/app/modelconfig"
	"feidex/internal/config"
	"feidex/internal/state"
)

func effectiveBindingForSession(a *App, sess *state.Session) *state.AgentBinding {
	return agentBindingForSession(a, sess)
}

func effectiveCodexModel(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		strings.TrimSpace(sessionModelOverride(sess)),
		strings.TrimSpace(bindingModelOverride(binding)),
		botProfileModelForApp(a),
		configuredGlobalModel(a.cfg),
	)
}

func effectiveCodexReasoningEffort(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		strings.TrimSpace(bindingReasoningEffortOverride(binding)),
		botProfileReasoningEffortForApp(a),
		modelconfig.ConfiguredGlobalReasoningEffort(a.cfg),
	)
}

func effectiveCodexPlanModel(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		sessionPlanModelOverride(sess), bindingPlanModelOverride(binding), botProfilePlanModelForApp(a), strings.TrimSpace(a.cfg.Codex.PlanModel), effectiveCodexModel(a, sess, nil),
	)
}

func effectiveCodexPlanReasoningEffort(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		sessionPlanReasoningEffortOverride(sess), bindingPlanReasoningEffortOverride(binding), botProfilePlanReasoningEffortForApp(a), strings.TrimSpace(a.cfg.Codex.PlanReasoningEffort),
	)
}

func effectiveCodexReviewModel(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(sessionReviewModelOverride(sess), bindingReviewModelOverride(binding), botProfileReviewModelForApp(a), strings.TrimSpace(a.cfg.Codex.ReviewModel), effectiveCodexModel(a, sess, nil))
}

func effectiveCodexSubagentModel(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(sessionSubagentModelOverride(sess), bindingSubagentModelOverride(binding), botProfileSubagentModelForApp(a), strings.TrimSpace(a.cfg.Codex.SubagentModel), effectiveCodexModel(a, sess, nil))
}

func effectiveCodexSubagentReasoningEffort(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(sessionSubagentReasoningEffortOverride(sess), bindingSubagentReasoningEffortOverride(binding), botProfileSubagentReasoningEffortForApp(a), strings.TrimSpace(a.cfg.Codex.SubagentReasoningEffort), effectiveCodexReasoningEffort(a, sess))
}

func effectiveClaudeModel(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		strings.TrimSpace(sessionModelOverride(sess)),
		strings.TrimSpace(bindingModelOverride(binding)),
		botProfileClaudeModelForApp(a),
		strings.TrimSpace(a.cfg.Claude.Model),
	)
}

func effectiveClaudeSmallModel(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	if profile := effectiveBotProfile(a); profile != nil {
		return firstNonEmpty(sessionSmallModelOverride(sess), bindingSmallModelOverride(binding), strings.TrimSpace(profile.ClaudeSmallModel), strings.TrimSpace(a.cfg.Claude.SmallModel))
	}
	return firstNonEmpty(sessionSmallModelOverride(sess), bindingSmallModelOverride(binding), strings.TrimSpace(a.cfg.Claude.SmallModel))
}

func effectiveClaudeSubagentModel(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(sessionSubagentModelOverride(sess), bindingSubagentModelOverride(binding), botProfileClaudeSubagentModelForApp(a), strings.TrimSpace(a.cfg.Claude.SubagentModel), effectiveClaudeModel(a, sess, nil))
}

func effectiveBindingApprovalPolicy(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	if sess != nil && strings.TrimSpace(sess.ActiveThreadApprovalPolicy) != "" {
		return strings.TrimSpace(sess.ActiveThreadApprovalPolicy)
	}
	if binding != nil && strings.TrimSpace(binding.ApprovalPolicyOverride) != "" {
		return strings.TrimSpace(binding.ApprovalPolicyOverride)
	}
	if profile := effectiveBotProfile(a); profile != nil && strings.TrimSpace(profile.ApprovalPolicy) != "" {
		return strings.TrimSpace(profile.ApprovalPolicy)
	}
	return effectiveThreadApprovalPolicy(sess, ws)
}

func effectiveBindingSandboxMode(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	if sess != nil && strings.TrimSpace(sess.ActiveThreadSandboxMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadSandboxMode)
	}
	if binding != nil && strings.TrimSpace(binding.SandboxModeOverride) != "" {
		return strings.TrimSpace(binding.SandboxModeOverride)
	}
	if profile := effectiveBotProfile(a); profile != nil && strings.TrimSpace(profile.SandboxMode) != "" {
		return strings.TrimSpace(profile.SandboxMode)
	}
	return effectiveThreadSandboxMode(sess, ws)
}

func effectiveBindingServiceTier(a *App, sess *state.Session) string {
	if serviceTier := effectiveThreadServiceTier(sess); strings.TrimSpace(serviceTier) != "" {
		return strings.TrimSpace(serviceTier)
	}
	if binding := effectiveBindingForSession(a, sess); binding != nil {
		if value := strings.TrimSpace(binding.ServiceTierOverride); value != "" {
			return value
		}
	}
	if profile := effectiveBotProfile(a); profile != nil {
		return strings.TrimSpace(profile.ServiceTier)
	}
	return ""
}

func effectiveBindingMultiAgentMode(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	if sess != nil && strings.TrimSpace(sess.ActiveThreadMultiAgentMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadMultiAgentMode)
	}
	if binding != nil && strings.TrimSpace(binding.MultiAgentModeOverride) != "" {
		return strings.TrimSpace(binding.MultiAgentModeOverride)
	}
	if profile := effectiveBotProfile(a); profile != nil && strings.TrimSpace(profile.MultiAgentMode) != "" {
		return strings.TrimSpace(profile.MultiAgentMode)
	}
	return effectiveThreadMultiAgentMode(sess, ws)
}

func effectiveBindingClaudePermissionMode(a *App, sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if binding := effectiveBindingForSession(a, sess); binding != nil && strings.TrimSpace(binding.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(binding.ClaudePermissionMode)
	}
	if profile := effectiveBotProfile(a); profile != nil && strings.TrimSpace(profile.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(profile.ClaudePermissionMode)
	}
	return effectiveClaudePermissionMode(sess, ws, cfg)
}

func botProfileModelForApp(a *App) string {
	if profile := effectiveBotProfile(a); profile != nil {
		return strings.TrimSpace(profile.Model)
	}
	return ""
}

func botProfileClaudeModelForApp(a *App) string {
	if profile := effectiveBotProfile(a); profile != nil {
		return strings.TrimSpace(profile.ClaudeModel)
	}
	return ""
}

func botProfileReasoningEffortForApp(a *App) string {
	if profile := effectiveBotProfile(a); profile != nil {
		return strings.TrimSpace(profile.ReasoningEffort)
	}
	return ""
}

func botProfilePlanModelForApp(a *App) string {
	if p := effectiveBotProfile(a); p != nil {
		return strings.TrimSpace(p.PlanModel)
	}
	return ""
}
func botProfilePlanReasoningEffortForApp(a *App) string {
	if p := effectiveBotProfile(a); p != nil {
		return strings.TrimSpace(p.PlanReasoningEffort)
	}
	return ""
}
func botProfileReviewModelForApp(a *App) string {
	if p := effectiveBotProfile(a); p != nil {
		return strings.TrimSpace(p.ReviewModel)
	}
	return ""
}
func botProfileSubagentModelForApp(a *App) string {
	if p := effectiveBotProfile(a); p != nil {
		return strings.TrimSpace(p.SubagentModel)
	}
	return ""
}
func botProfileSubagentReasoningEffortForApp(a *App) string {
	if p := effectiveBotProfile(a); p != nil {
		return strings.TrimSpace(p.SubagentReasoningEffort)
	}
	return ""
}
func botProfileClaudeSubagentModelForApp(a *App) string {
	if p := effectiveBotProfile(a); p != nil {
		return strings.TrimSpace(p.ClaudeSubagentModel)
	}
	return ""
}

func sessionModelOverride(sess *state.Session) string {
	if sess == nil {
		return ""
	}
	return sess.ModelOverride
}

func bindingModelOverride(binding *state.AgentBinding) string {
	if binding == nil {
		return ""
	}
	return binding.ModelOverride
}

func bindingReasoningEffortOverride(binding *state.AgentBinding) string {
	if binding == nil {
		return ""
	}
	return binding.ReasoningEffortOverride
}

func bindingPlanModelOverride(b *state.AgentBinding) string {
	if b != nil {
		return b.PlanModelOverride
	}
	return ""
}
func bindingPlanReasoningEffortOverride(b *state.AgentBinding) string {
	if b != nil {
		return b.PlanReasoningEffortOverride
	}
	return ""
}
func bindingReviewModelOverride(b *state.AgentBinding) string {
	if b != nil {
		return b.ReviewModelOverride
	}
	return ""
}
func bindingSubagentModelOverride(b *state.AgentBinding) string {
	if b != nil {
		return b.SubagentModelOverride
	}
	return ""
}
func bindingSubagentReasoningEffortOverride(b *state.AgentBinding) string {
	if b != nil {
		return b.SubagentReasoningEffortOverride
	}
	return ""
}
func bindingSmallModelOverride(b *state.AgentBinding) string {
	if b != nil {
		return b.SmallModelOverride
	}
	return ""
}
func sessionPlanModelOverride(s *state.Session) string {
	if s != nil {
		return s.PlanModelOverride
	}
	return ""
}
func sessionPlanReasoningEffortOverride(s *state.Session) string {
	if s != nil {
		return s.PlanReasoningEffortOverride
	}
	return ""
}
func sessionReviewModelOverride(s *state.Session) string {
	if s != nil {
		return s.ReviewModelOverride
	}
	return ""
}
func sessionSubagentModelOverride(s *state.Session) string {
	if s != nil {
		return s.SubagentModelOverride
	}
	return ""
}
func sessionSubagentReasoningEffortOverride(s *state.Session) string {
	if s != nil {
		return s.SubagentReasoningEffortOverride
	}
	return ""
}
func sessionSmallModelOverride(s *state.Session) string {
	if s != nil {
		return s.SmallModelOverride
	}
	return ""
}

func codexAuxiliaryConfig(a *App, sess *state.Session) map[string]any {
	if a == nil {
		return nil
	}
	result := map[string]any{}
	if value := effectiveCodexReviewModel(a, sess); value != "" {
		result["review_model"] = value
	}
	if value := effectiveCodexSubagentModel(a, sess); value != "" {
		result["agents.default_subagent_model"] = value
	}
	if value := effectiveCodexSubagentReasoningEffort(a, sess); value != "" {
		result["agents.default_subagent_reasoning_effort"] = value
	}
	return result
}
