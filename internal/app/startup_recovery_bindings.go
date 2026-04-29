package app

import (
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/state"
)

func recoverRuntimeState(a *App) {
	newRuntimeMaintenanceService(a).RecoverRuntimeState()
}

func recoverSharedRuntimeState(a *App) {
	newRuntimeMaintenanceService(a).RecoverSharedRuntimeState()
}

func recoverFrontendRuntimeState(a *App) {
	newRuntimeMaintenanceService(a).RecoverFrontendRuntimeState()
}

func resetLiveThreadState(a *App) {
	if a == nil {
		return
	}
	a.liveThreads = newLiveThreadTracker()
}

func startupReadyChatIDs(sessions []*state.Session) []string {
	return appmaintenance.StartupReadyChatIDs(sessions)
}

func appStartupReadyChatIDs(a *App, sessions []*state.Session) []string {
	return newRuntimeMaintenanceService(a).FrontendStartupReadyChatIDs(sessions)
}

func sendStartupReadyNotifications(a *App) {
	newRuntimeMaintenanceService(a).SendStartupReadyNotifications()
}
