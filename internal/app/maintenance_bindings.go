package app

import (
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/config"
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

func (a *App) QueueFrontendCardNotification(note state.FrontendCardNotification) {
	queueFrontendCardNotification(a, note)
}

func (a *App) MaintenanceResetLiveThreadState() {
	if a == nil {
		return
	}
	a.liveThreads = newLiveThreadTracker()
}

func (a *App) MaintenanceWithFrontendRecoveryLock(fn func()) {
	if a == nil {
		return
	}
	a.frontendRecoveryMu.Lock()
	defer a.frontendRecoveryMu.Unlock()
	if fn != nil {
		fn()
	}
}

func (a *App) MaintenanceBeginBackendStartupRecovery() func() {
	if a == nil {
		return func() {}
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.beginStartupRecoveryScope(a)
	}
	return func() {}
}

func (a *App) MaintenanceSessionBelongsToFrontend(sessionKey string) bool {
	return sessionBelongsToFrontend(a, sessionKey)
}

func (a *App) MaintenanceClearSessionThreadContext(sess *state.Session) {
	clearSessionThreadContext(sess)
}

func (a *App) MaintenanceResetSessionActiveOperations(sess *state.Session) {
	sessionResetActiveOperations(sess)
}

func (a *App) MaintenanceSessionHasInFlightSubmission(sess *state.Session) bool {
	return sessionHasInFlightSubmission(sess)
}

func (a *App) MaintenanceClearSessionLiveThread(sessionKey string) {
	clearSessionLiveThread(a, sessionKey)
}

func (a *App) MaintenanceConfiguredGlobalModel() string {
	if a == nil {
		return ""
	}
	return configuredGlobalModel(a.cfg)
}

func (a *App) MaintenanceRecoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	conversationBackend(a).RecoverStartupConversation(sessionKey, workspaceID, sess, ws, effectiveModel)
}
