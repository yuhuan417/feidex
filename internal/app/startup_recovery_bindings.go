package app

import (
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
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

func startupReadyChatIDs(sessions []*conversation.Session) []string {
	return appmaintenance.StartupReadyChatIDs(sessions)
}

func sendStartupReadyNotifications(a *App) {
	appmaintenance.NewRuntimeMaintenanceService(a).SendStartupReadyNotifications()
}
