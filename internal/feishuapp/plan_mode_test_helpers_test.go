package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/state"
)

func codexPlanModeExitPendingRequest(a *App, sessionKey string) *state.PendingRequest {
	return planmode.ExitPendingRequest(newPlanModeAppAdapter(a), sessionKey)
}

func clearCodexPlanModeForSession(a *App, sessionKey string) (bool, error) {
	return planmode.ClearCodexPlanModeForSession(newPlanModeAppAdapter(a), sessionKey)
}
