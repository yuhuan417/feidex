package app

import (
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Maintenance provider adapters
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// *App methods satisfying maintenance.App
// ---------------------------------------------------------------------------

func (a *App) MaintenanceAppState() appmaintenance.AppStateProvider {
	if a == nil {
		return nil
	}
	return a.State()
}

func (a *App) MaintenanceRuntimeState() appmaintenance.RuntimeStateProvider {
	return newRuntimeStateService(a)
}

func (a *App) AppStartupReadyChatIDs(sessions []*state.Session) []string {
	return appStartupReadyChatIDs(a, sessions)
}

func (a *App) QueueFrontendCardNotification(note state.FrontendCardNotification) {
	queueFrontendCardNotification(a, note)
}
