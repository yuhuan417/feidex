package feishuapp

import (
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/frontend"
	frontendruntime "feidex/internal/runtime"
)

func frontendIsIdle(query frontend.Query) bool { return frontendIdleBlockedReason(query) == "" }
func frontendIdleBlockedReason(query frontend.Query) string {
	return frontendIdleBlockedReasonWithMessageTrafficAllowance(query, 0)
}
func frontendIdleBlockedReasonIgnoringCurrentMessage(query frontend.Query) string {
	return frontendIdleBlockedReasonWithMessageTrafficAllowance(query, 1)
}

func frontendActivity(frontendquery frontend.Query, includeSessions bool) frontend.Activity {
	return frontendquery.Activity(includeSessions)
}

func FrontendFacts(owner *frontendruntime.FrontendOwner, maintenanceService backendmaintenance.MaintenanceStateService) func() frontend.RuntimeFacts {
	return func() frontend.RuntimeFacts {
		return frontend.RuntimeFacts{SwitchBlockedReason: owner.BackendTransition.BackendSwitchBlockedReasonForTraffic(), MessageTraffic: owner.MessageTraffic(), CodexMaintenance: maintenanceService.CodexMaintenanceActive(), ClaudeMaintenance: maintenanceService.ClaudeMaintenanceActive()}
	}
}

func frontendIdleBlockedReasonWithMessageTrafficAllowance(query frontend.Query, allowance int) string {
	if query.Repository == nil {
		return "app not initialized"
	}
	return frontendActivity(query, true).IdleBlockedReason(allowance)
}
