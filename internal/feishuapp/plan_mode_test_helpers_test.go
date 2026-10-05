package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/state"
)

func codexPlanModeExitPendingRequest(a *Frontend, sessionKey string) *state.PendingRequest {
	return planmode.ExitPendingRequest(a.bindings.PlanMode, sessionKey)
}

func clearCodexPlanModeForSession(a *Frontend, sessionKey string) (bool, error) {
	return planmode.ClearCodexPlanModeForSession(a.bindings.PlanMode, sessionKey)
}
