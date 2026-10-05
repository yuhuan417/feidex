package feishuapp

import (
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/codexrpc"
)

func buildTurnSandboxPolicy(mode string) map[string]any { return codexadapter.SandboxPolicy(mode) }
func codexCollaborationModeForTurnStart(a *Frontend, sessionKey, threadID string) *codexrpc.CollaborationMode {
	return codexadapter.CollaborationModeFromState(planModeStateForTurnStart(a.bindings.Plan, sessionKey, threadID))
}
