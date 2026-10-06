package feishuapp

import (
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/domain/conversation"

	"feidex/internal/config"
)

func effectiveBindingClaudePermissionMode(store *appstate.Store, sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	return conversation.ResolveSettings(sess, agentBindingForSession(store, sess), effectiveBotProfile(store), ws, cfg.PermissionMode).ClaudePermissionMode
}
