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

func frontendActivity(frontendquery frontend.Query, includeSessions bool) frontend.Activity {
	return frontendquery.Activity(includeSessions)
}

func FrontendFacts(a *App) func() frontend.RuntimeFacts {
	// Read once at construction so the dependency is visible.
	maintenanceService := a.bindings.Maintenance

	return func() frontend.RuntimeFacts {
		return frontend.RuntimeFacts{SwitchBlockedReason: a.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic(), MessageTraffic: a.runtimeOwner.MessageTraffic(), CodexMaintenance: maintenanceService.CodexMaintenanceActive(), ClaudeMaintenance: maintenanceService.ClaudeMaintenanceActive()}
	}
}

func frontendIdleBlockedReasonWithMessageTrafficAllowance(a *App, allowance int) string {
	if a == nil {
		return "app not initialized"
	}
	return frontendActivity(a.bindings.FrontendQuery, true).IdleBlockedReason(allowance)
}
