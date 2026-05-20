package app

import (
	"context"
	"strings"
	"time"

	appcodexruntime "feidex/internal/app/codexruntime"
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
		RecoverFrontendRuntimeState: func() {
			recoverFrontendRuntimeState(a)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "升级前检查失败: "+err.Error())
		return
	}
	if !probe.Supported {
		finalize("failed", "当前环境不支持 Codex 自升级: "+firstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previousVersion := firstNonEmpty(probe.CurrentVersion, payload.CurrentVersion)
	targetVersion := firstNonEmpty(payload.TargetVersion, "latest")
	updateCommand := firstNonEmpty(probe.UpdateCommand, payload.UpdateCommand, "update")
	newMaintenanceStateService(s.app).UpdateCodexUpgrade(func(snapshot *backendUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = targetVersion
		snapshot.LatestVersion = targetVersion
	})
	if reason := newMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}

	update("installing", "正在运行 Codex 自升级命令 `"+firstNonEmpty(probe.Command, "codex")+" "+updateCommand+"`")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = manager.InstallVersion(ctx, targetVersion)
	cancel()
	if err != nil {
		finalize("failed", "Codex 自升级失败，未自动回滚: "+err.Error())
		return
	}

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	afterProbe, probeErr := manager.Probe(ctx)
	cancel()
	installedVersion := previousVersion
	if probeErr == nil {
		installedVersion = firstNonEmpty(afterProbe.CurrentVersion, installedVersion)
	}
	if strings.TrimSpace(installedVersion) != "" {
		newMaintenanceStateService(s.app).UpdateCodexUpgrade(func(snapshot *backendUpgradeSnapshot) {
			snapshot.TargetVersion = installedVersion
			snapshot.LatestVersion = installedVersion
		})
	}
	if probeErr != nil {
		finalize("failed", "Codex 自升级后版本检查失败，未自动回滚: "+probeErr.Error())
		return
	}

	update("smoke_testing", "正在验证 Codex runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := newBackendUpgradeService(s.app).refreshCodexRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		finalize("failed", "Codex 自升级后 runtime 验证失败，未自动回滚: "+err.Error())
		return
	}
	if strings.TrimSpace(installedVersion) != "" && strings.TrimSpace(installedVersion) == strings.TrimSpace(previousVersion) {
		if switched {
			finalize("success", "Codex 已是最新版本 `"+installedVersion+"`，runtime 已重新验证")
			return
		}
		finalize("success", "Codex 已是最新版本 `"+installedVersion+"`；当前 frontend 未启用 Codex backend")
		return
	}
	if switched {
		finalize("success", "Codex 自升级成功，已切换到 `"+firstNonEmpty(installedVersion, targetVersion)+"`")
		return
	}
	finalize("success", "Codex 自升级成功，已验证 `"+firstNonEmpty(installedVersion, targetVersion)+"` 可用；当前 frontend 未启用 Codex backend")
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
