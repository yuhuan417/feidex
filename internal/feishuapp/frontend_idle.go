package feishuapp

import (
	"feidex/internal/application/frontend"
)

func frontendIsIdle(a *App) bool { return frontendIdleBlockedReason(a) == "" }
func frontendIdleBlockedReason(a *App) string {
	return frontendIdleBlockedReasonWithMessageTrafficAllowance(a, 0)
}
func frontendIdleBlockedReasonIgnoringCurrentMessage(a *App) string {
	return frontendIdleBlockedReasonWithMessageTrafficAllowance(a, 1)
}

func frontendActivity(a *App, includeSessions bool) frontend.Activity {
	return a.bindings.FrontendQuery.Activity(includeSessions)
}

func FrontendFacts(a *App) func() frontend.RuntimeFacts {
	return func() frontend.RuntimeFacts {
		return frontend.RuntimeFacts{SwitchBlockedReason: a.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic(), MessageTraffic: a.runtimeOwner.MessageTraffic(), CodexMaintenance: a.bindings.Maintenance.CodexMaintenanceActive(), ClaudeMaintenance: a.bindings.Maintenance.ClaudeMaintenanceActive()}
	}
}

func frontendIdleBlockedReasonWithMessageTrafficAllowance(a *App, allowance int) string {
	if a == nil {
		return "app not initialized"
	}
	return frontendActivity(a, true).IdleBlockedReason(allowance)
}
