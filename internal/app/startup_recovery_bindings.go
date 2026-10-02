package app

import (
	appmaintenance "feidex/internal/app/maintenance"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

func recoverRuntimeState(a *App) {
	appmaintenance.NewRuntimeMaintenanceService(a).RecoverRuntimeState()
}

func recoverSharedRuntimeState(a *App) {
	appmaintenance.NewRuntimeMaintenanceService(a).RecoverSharedRuntimeState()
}

func recoverFrontendRuntimeState(a *App) {
	appmaintenance.NewRuntimeMaintenanceService(a).RecoverFrontendRuntimeState()
}

func resetLiveThreadState(a *App) {
	if a == nil {
		return
	}
	a.liveThreads = frontendruntime.NewLiveThreads()
}

func startupReadyChatIDs(sessions []*state.Session) []string {
	return appmaintenance.StartupReadyChatIDs(sessions)
}

func sendStartupReadyNotifications(a *App) {
	appmaintenance.NewRuntimeMaintenanceService(a).SendStartupReadyNotifications()
}
