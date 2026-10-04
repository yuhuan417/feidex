package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"

	"context"
	"sync"

	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	appcodexruntime "feidex/internal/runtime/codex"
)

// CodexUpgradePorts builds upgrade dependencies from frontend-owned values.
// The runtime snapshot includes the already constructed Codex recovery service
// used by newly created clients' transport handlers.
func CodexUpgradePorts(
	cfg *config.Config,
	configMu *sync.RWMutex,
	frontendID string,
	frontendConfigIndex int,
	runtimeOwner *frontendruntime.FrontendOwner,
	backendRuntime BackendRuntimeDeps,
	codexRecovery appcodexruntime.RecoveryService,
	recoverFrontendRuntimeState func(),
	codexSmokeTest func(context.Context) error,
) appcodexruntime.UpgradeDependencies {
	configSnapshot := func() config.CodexConfig {
		if cfg == nil {
			return config.CodexConfig{}
		}
		if configMu != nil {
			configMu.RLock()
			defer configMu.RUnlock()
		}
		return cfg.Codex
	}
	backendIsActive := func() bool {
		backend := ""
		if runtimeOwner != nil {
			backend = runtimeOwner.Backend()
		}
		view := frontendConfigView{
			cfg: cfg, mu: configMu, backend: backend,
			frontendID: frontendID, frontendConfigIndex: frontendConfigIndex,
		}
		return view.configuredBackend() == domainbackend.BackendCodex
	}

	return appcodexruntime.UpgradeDependencies{
		CreateClient: func() appcodexruntime.CodexClient {
			return newCodexClient(configSnapshot())
		},
		ConfigureClient: func(client appcodexruntime.CodexClient) {
			runtimeDeps := backendRuntime
			if runtimeOwner != nil {
				runtimeDeps.view.backend = runtimeOwner.Backend()
			}
			configureCodexClientRuntime(runtimeDeps, client)
		},
		ClientExperimentalAPI: func() bool {
			return configSnapshot().ExperimentalAPI
		},
		IsBackendActive: backendIsActive,
		SmokeTest:       codexSmokeTest,
		CurrentClient: func() appcodexruntime.CodexClient {
			if runtimeOwner == nil {
				return nil
			}
			return (runtimeView{owner: runtimeOwner}).currentCodexClient()
		},
		ReplaceClient: func(next appcodexruntime.CodexClient) appcodexruntime.CodexClient {
			return replaceCodexClient(codexRecovery, next)
		},
		RecoverFrontendRuntimeState: recoverFrontendRuntimeState,
	}
}

func (s backendUpgradeService) refreshCodexRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	return s.codexUpgrade.RefreshRuntimeAfterMaintenance(ctx)
}

func (s backendUpgradeService) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.sessionKey,
		msg,
		s.replyCardWithID,
		s.replyInThread(),
		s.backendMaintenance["codex"].BeginRestart,
		func(messageID, sessionKey string) {
			_ = s.maintenanceRunners["codex"].Start(backendmaintenance.Operation{MessageID: messageID, SessionKey: sessionKey, Restart: true})
		},
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return s.presentation.renderRestartOperationCard(upgraderender.CodexSpec, sessionKey, snapshot)
		},
		func(message string) {
			s.maintenance.FinishCodexRestart("failed", message)
		},
	)
}
