package feishuapp

import (
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

func effectiveBindingForSession(a *App, sess *conversation.Session) *state.AgentBinding {
	return agentBindingForSession(a, sess)
}

func effectiveBindingApprovalPolicy(a *App, sess *conversation.Session, ws *config.Workspace) string {
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

func effectiveBindingSandboxMode(a *App, sess *conversation.Session, ws *config.Workspace) string {
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

func effectiveBindingServiceTier(a *App, sess *conversation.Session) string {
	if serviceTier := conversation.EffectiveServiceTier(sess); strings.TrimSpace(serviceTier) != "" {
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

func effectiveBindingMultiAgentMode(a *App, sess *conversation.Session, ws *config.Workspace) string {
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

func effectiveBindingClaudePermissionMode(a *App, sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
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

func codexAuxiliaryConfig(a *App, sess *conversation.Session) map[string]any {
	if a == nil {
		return nil
	}
	snapshot := modelConfigSnapshot(a, sess, domainbackend.BackendCodex)
	result := map[string]any{}
	if snapshot.ReviewModel != "" {
		result["review_model"] = snapshot.ReviewModel
	}
	if snapshot.SubagentModel != "" {
		result["agents.default_subagent_model"] = snapshot.SubagentModel
	}
	if snapshot.SubagentEffort != "" {
		result["agents.default_subagent_reasoning_effort"] = snapshot.SubagentEffort
	}
	return result
}
