package feishuapp

import (
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/frontend"
	frontendruntime "feidex/internal/runtime"
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

func FrontendFacts(owner *frontendruntime.FrontendOwner, maintenanceService backendmaintenance.MaintenanceStateService) func() frontend.RuntimeFacts {
	return func() frontend.RuntimeFacts {
		return frontend.RuntimeFacts{SwitchBlockedReason: owner.BackendTransition.BackendSwitchBlockedReasonForTraffic(), MessageTraffic: owner.MessageTraffic(), CodexMaintenance: maintenanceService.CodexMaintenanceActive(), ClaudeMaintenance: maintenanceService.ClaudeMaintenanceActive()}
	}
}

func frontendIdleBlockedReasonWithMessageTrafficAllowance(a *App, allowance int) string {
	if a == nil {
		return "app not initialized"
	}
	return frontendActivity(a.bindings.FrontendQuery, true).IdleBlockedReason(allowance)
}
