package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/feishu"
)

func (s backendUpgradeService) startClaudeRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.sessionKey,
		msg,
		s.replyCardWithID,
		s.replyInThread(),
		s.backendMaintenance["claude"].BeginRestart,
		func(messageID, sessionKey string) {
			_ = s.maintenanceRunners["claude"].Start(backendmaintenance.Operation{MessageID: messageID, SessionKey: sessionKey, Restart: true})
		},
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return s.presentation.renderRestartOperationCard(upgraderender.ClaudeSpec, sessionKey, snapshot)
		},
		func(message string) {
			s.maintenance.FinishClaudeRestart("failed", message)
		},
	)
}
