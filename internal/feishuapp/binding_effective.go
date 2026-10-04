package feishuapp

import (
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/domain/conversation"

	"feidex/internal/config"
	"feidex/internal/state"
)

func effectiveBindingClaudePermissionMode(store *appstate.Store, sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	return conversation.ResolveSettings(sess, agentBindingForSession(store, sess), effectiveBotProfile(store), ws, cfg.PermissionMode).ClaudePermissionMode
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
