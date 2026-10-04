package feishuapp

import (
	"feidex/internal/domain/conversation"

	"feidex/internal/config"
	"feidex/internal/state"
)

func effectiveBindingClaudePermissionMode(a *App, sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	return conversation.ResolveSettings(sess, agentBindingForSession(a, sess), effectiveBotProfile(a.State()), ws, cfg.PermissionMode).ClaudePermissionMode
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
