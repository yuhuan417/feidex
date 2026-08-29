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
		configuredGlobalModel(a.cfg),
	)
}

func effectiveCodexReasoningEffort(a *App, sess *state.Session) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		strings.TrimSpace(bindingReasoningEffortOverride(binding)),
		modelconfig.ConfiguredGlobalReasoningEffort(a.cfg),
	)
}

func effectiveClaudeModel(a *App, sess *state.Session, ws *config.Workspace) string {
	binding := effectiveBindingForSession(a, sess)
	return firstNonEmpty(
		strings.TrimSpace(sessionModelOverride(sess)),
		strings.TrimSpace(bindingModelOverride(binding)),
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
	return effectiveThreadSandboxMode(sess, ws)
}

func effectiveBindingServiceTier(a *App, sess *state.Session) string {
	if serviceTier := effectiveThreadServiceTier(sess); strings.TrimSpace(serviceTier) != "" {
		return strings.TrimSpace(serviceTier)
	}
	if binding := effectiveBindingForSession(a, sess); binding != nil {
		return strings.TrimSpace(binding.ServiceTierOverride)
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
	return effectiveThreadMultiAgentMode(sess, ws)
}

func effectiveBindingClaudePermissionMode(a *App, sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if binding := effectiveBindingForSession(a, sess); binding != nil && strings.TrimSpace(binding.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(binding.ClaudePermissionMode)
	}
	return effectiveClaudePermissionMode(sess, ws, cfg)
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
