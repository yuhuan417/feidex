package app

import (
	"context"
	"strings"
	"time"

	appcodexruntime "feidex/internal/app/codexruntime"
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/feishu"
)

// newCodexUpgradeService builds a codexruntime.UpgradeService with
// all callbacks wired to *App dependencies.
func newCodexUpgradeService(a *App) appcodexruntime.UpgradeService {
	return appcodexruntime.UpgradeService{
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
			if runtime := backendRuntimeForKind(backendCodex); runtime != nil {
				return runtime.isActive(a)
			}
			return false
		},
		SmokeTest: func(ctx context.Context) error {
			return newCodexUpgradeService(a).CodexSmokeTest(ctx)
		},
		CurrentClient: func() appcodexruntime.CodexClient {
			return currentCodexClient(a)
		},
		ReplaceClient: func(next appcodexruntime.CodexClient) appcodexruntime.CodexClient {
			return replaceCodexClient(a, next)
		},
	}
}

func (s backendUpgradeService) runCodexUpgradeOperation(messageID, sessionKey string, payload codexUpgradePendingPayload) {
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"codex upgrade progress patch failed",
		newUpgradeRenderService(s.app).renderCodexUpgradeOperationCard,
		newMaintenanceStateService(s.app).UpdateCodexUpgrade,
		newMaintenanceStateService(s.app).FinishCodexUpgrade,
		func(snapshot *backendUpgradeSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)
	appmaintenance.RunUpgradeWorkflow(appmaintenance.UpgradeWorkflow{
		PackageName:    "@openai/codex",
		BackendName:    "Codex",
		CurrentVersion: payload.CurrentVersion,
		TargetVersion:  payload.TargetVersion,
		Probe: func(ctx context.Context) (appmaintenance.UpgradeProbe, error) {
			probe, err := manager.Probe(ctx)
			return appmaintenance.UpgradeProbe{Supported: probe.Supported, Reason: probe.Reason, CurrentVersion: probe.CurrentVersion}, err
		},
		InstallVersion:    manager.InstallVersion,
		SmokeTest:         s.codexSmokeTest,
		RefreshRuntime:    s.refreshCodexRuntimeAfterMaintenance,
		RuntimeBusyReason: newMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason,
		RecordVersions: func(previousVersion, targetVersion string) {
			newMaintenanceStateService(s.app).UpdateCodexUpgrade(func(snapshot *backendUpgradeSnapshot) {
				snapshot.CurrentVersion = previousVersion
				snapshot.PreviousVersion = previousVersion
				snapshot.TargetVersion = targetVersion
				snapshot.LatestVersion = targetVersion
			})
		},
		Update:   func(phase, message string) { update(phase, message) },
		Finalize: finalize,
	})
}

func (s backendUpgradeService) codexSmokeTest(ctx context.Context) error {
	return newCodexUpgradeService(s.app).CodexSmokeTest(ctx)
}

func (s backendUpgradeService) startVerifiedCodexClient(ctx context.Context) (CodexClient, error) {
	return newCodexUpgradeService(s.app).StartVerifiedCodexClient(ctx)
}

func (s backendUpgradeService) refreshCodexRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	return newCodexUpgradeService(s.app).RefreshRuntimeAfterMaintenance(ctx)
}

func (s backendUpgradeService) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.app,
		msg,
		newBackendUpgradeService(s.app).beginCodexRestartOperation,
		newBackendUpgradeService(s.app).runCodexRestartOperation,
		newUpgradeRenderService(s.app).renderCodexRestartOperationCard,
		func(message string) { newMaintenanceStateService(s.app).FinishCodexRestart("failed", message) },
	)
}

func (s backendUpgradeService) beginCodexRestartOperation() (backendRestartSnapshot, error) {
	if err := newMaintenanceStateService(s.app).EnsureCodexUpgradeReady(); err != nil {
		return backendRestartSnapshot{}, err
	}
	snapshot := backendRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(newMaintenanceStateService(s.app).CodexUpgradeState().CurrentVersion, newMaintenanceStateService(s.app).CodexRestartState().CurrentVersion),
	}
	if !newMaintenanceStateService(s.app).BeginCodexRestart(snapshot) {
		return backendRestartSnapshot{}, errString("Codex 正在维护中，请稍后再试")
	}
	return newMaintenanceStateService(s.app).CodexRestartState(), nil
}

func (s backendUpgradeService) runCodexRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"codex restart progress patch failed",
		newUpgradeRenderService(s.app).renderCodexRestartOperationCard,
		newMaintenanceStateService(s.app).UpdateCodexRestart,
		newMaintenanceStateService(s.app).FinishCodexRestart,
		func(snapshot *backendRestartSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	newMaintenanceStateService(s.app).UpdateCodexRestart(func(snapshot *backendRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := newMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在准备新的 Codex runtime")
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := newBackendUpgradeService(s.app).refreshCodexRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		finalize("failed", "Codex runtime 重启失败: "+err.Error())
		return
	}
	if switched {
		finalize("success", "Codex runtime 已原地重启，后续任务会使用新进程")
		return
	}
	finalize("success", "Codex CLI 校验通过；当前 frontend 未启用 Codex backend")
}
