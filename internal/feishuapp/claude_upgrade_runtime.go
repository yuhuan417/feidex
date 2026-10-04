package feishuapp

import (
	"context"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/feishu"
)

func (s backendUpgradeService) startClaudeRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.app.configView(),
		msg,
		func(ctx context.Context, parent string, card map[string]any, inThread bool) (string, error) {
			return replyCardWithIDEffect(ctx, s.app, parent, card, inThread)
		},
		s.app.configView().replyInThreadEnabled(),
		s.app.bindings.BackendMaintenance["claude"].BeginRestart,
		func(messageID, sessionKey string) {
			_ = s.app.bindings.MaintenanceRunners["claude"].Start(backendmaintenance.Operation{MessageID: messageID, SessionKey: sessionKey, Restart: true})
		},
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return s.app.bindings.UpgradePresentation.renderRestartOperationCard(upgraderender.ClaudeSpec, sessionKey, snapshot)
		},
		func(message string) {
			s.app.bindings.Maintenance.FinishClaudeRestart("failed", message)
		},
	)
}
