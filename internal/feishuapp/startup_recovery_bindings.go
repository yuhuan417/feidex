package feishuapp

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/runtime/maintenance"
)

func recoverRuntimeState(startuprecovery maintenance.StartupRecovery) {
	startuprecovery.RecoverRuntimeState()
}

func recoverFrontendRuntimeState(startuprecovery maintenance.StartupRecovery) {
	startuprecovery.RecoverFrontendRuntimeState()
}

func startupReadyChatIDs(sessions []*conversation.Session) []string {
	return maintenance.StartupReadyChatIDs(sessions)
}
