package app

import (
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/runtime/maintenance"
)

func recoverRuntimeState(a *App) {
	newStartupRecovery(a).RecoverRuntimeState()
}

func recoverSharedRuntimeState(a *App) {
	newStartupRecovery(a).RecoverSharedRuntimeState()
}

func recoverFrontendRuntimeState(a *App) {
	newStartupRecovery(a).RecoverFrontendRuntimeState()
}

func resetLiveThreadState(a *App) {
	if a == nil {
		return
	}
	a.liveThreads = frontendruntime.NewLiveThreads()
}

func startupReadyChatIDs(sessions []*conversation.Session) []string {
	return maintenance.StartupReadyChatIDs(sessions)
}

func sendStartupReadyNotifications(a *App) {
	newStartupRecovery(a).SendStartupReadyNotifications()
}
