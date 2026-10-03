package feishuapp

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/runtime/maintenance"
)

func recoverRuntimeState(a *App) {
	a.bindings.StartupRecovery.RecoverRuntimeState()
}

func recoverFrontendRuntimeState(a *App) {
	a.bindings.StartupRecovery.RecoverFrontendRuntimeState()
}

func resetLiveThreadState(a *App) {
	if a == nil {
		return
	}
	resetAppLiveThreadTracker(a)
}

func startupReadyChatIDs(sessions []*conversation.Session) []string {
	return maintenance.StartupReadyChatIDs(sessions)
}

func sendStartupReadyNotifications(a *App) {
	if a == nil || a.bindings == nil {
		return
	}
	a.bindings.StartupRecovery.SendStartupReadyNotifications()
}
