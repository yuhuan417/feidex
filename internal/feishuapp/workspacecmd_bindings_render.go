package feishuapp

import (
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/state"
)

func pendingBindingMessagePreview(pending *state.AgentBindingPendingMessage) string {
	return workspaceapp.PendingPreview(pending)
}
