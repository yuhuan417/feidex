package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application/backendmaintenance"
	domainbackend "feidex/internal/domain/backend"

	"context"

	"feidex/internal/feishu"
	appcodexruntime "feidex/internal/runtime/codex"
)

// newCodexUpgradeService builds a codexruntime.UpgradeService with
// all callbacks wired to *App dependencies.
func CodexUpgradePorts(a *App) appcodexruntime.UpgradeDependencies {
	return appcodexruntime.UpgradeDependencies{
		CreateClient: func() appcodexruntime.CodexClient {
			return newCodexClient(a.cfg.Codex)
		},
		ConfigureClient: func(client appcodexruntime.CodexClient) {
			configureCodexClientRuntime(a, client)
		},
		ClientExperimentalAPI: func() bool {
			return a.cfg.Codex.ExperimentalAPI
		},
		IsBackendActive: func() bool {
			return a.configView().configuredBackend() == domainbackend.BackendCodex
		},
		SmokeTest: func(ctx context.Context) error {
			return a.bindings.CodexUpgrade.CodexSmokeTest(ctx)
		},
		CurrentClient: func() appcodexruntime.CodexClient {
			return a.runtimeView().currentCodexClient()
		},
		ReplaceClient: func(next appcodexruntime.CodexClient) appcodexruntime.CodexClient {
			return replaceCodexClient(a.bindings.CodexRecovery, next)
		},
		RecoverFrontendRuntimeState: func() {
			recoverFrontendRuntimeState(a.bindings.StartupRecovery)
		},
	}
}

func (s backendUpgradeService) refreshCodexRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	return s.app.bindings.CodexUpgrade.RefreshRuntimeAfterMaintenance(ctx)
}

func (s backendUpgradeService) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.app,
		msg,
		s.app.bindings.BackendMaintenance["codex"].BeginRestart,
		func(messageID, sessionKey string) {
			_ = s.app.bindings.MaintenanceRunners["codex"].Start(backendmaintenance.Operation{MessageID: messageID, SessionKey: sessionKey, Restart: true})
		},
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return s.app.bindings.UpgradePresentation.renderRestartOperationCard(upgraderender.CodexSpec, sessionKey, snapshot)
		},
		func(message string) {
			s.app.bindings.Maintenance.FinishCodexRestart("failed", message)
		},
	)
}
