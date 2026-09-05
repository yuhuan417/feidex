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

func effectiveClaudeModel(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		strings.TrimSpace(sessionModelOverride(sess)),
		strings.TrimSpace(bindingModelOverride(binding)),
		botProfileClaudeModelForApp(a),
		strings.TrimSpace(a.cfg.Claude.Model),
	)
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
