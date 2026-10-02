package app

import (
	appbackend "feidex/internal/app/backend"
	"feidex/internal/app/upgraderender"
	appruntime "feidex/internal/runtime"
	"feidex/internal/textutil"

	"context"
	"strings"
	"time"

	"feidex/internal/feishu"
	appcodexruntime "feidex/internal/runtime/codex"
)

const cliSelfUpdateInstallTarget = "latest"

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
				return runtime.isActive(backendRuntimeContextForApp(a))
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

func (s backendUpgradeService) runCodexUpgradeOperation(messageID, sessionKey string, payload appruntime.BackendUpgradePendingPayload) {
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"codex upgrade progress patch failed",
		func(sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
			return newUpgradeRenderService(s.app).renderUpgradeOperationCard(upgraderender.CodexSpec, sessionKey, snapshot)
		},
		appbackend.NewMaintenanceStateService(s.app).UpdateCodexUpgrade,
		appbackend.NewMaintenanceStateService(s.app).FinishCodexUpgrade,
		func(snapshot *appbackend.BackendUpgradeSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)
	ctx, cancel := context.WithTimeout(s.app.Context(), 30*time.Second)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "升级前检查失败: "+err.Error())
		return
	}
	if !probe.Supported {
		finalize("failed", "当前环境不支持 Codex 自升级: "+textutil.FirstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previousVersion := textutil.FirstNonEmpty(probe.CurrentVersion, payload.CurrentVersion)
	targetVersion := textutil.FirstNonEmpty(payload.TargetVersion, "latest")
	updateCommand := textutil.FirstNonEmpty(probe.UpdateCommand, payload.UpdateCommand, "update")
	appbackend.NewMaintenanceStateService(s.app).UpdateCodexUpgrade(func(snapshot *appbackend.BackendUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = targetVersion
		snapshot.LatestVersion = targetVersion
	})
	if reason := appbackend.NewMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}

	update("installing", "正在运行 Codex 自升级命令 `"+textutil.FirstNonEmpty(probe.Command, "codex")+" "+updateCommand+"`")
	ctx, cancel = context.WithTimeout(s.app.Context(), 5*time.Minute)
	err = manager.InstallVersion(ctx, cliSelfUpdateInstallTarget)
	cancel()
	if err != nil {
		finalize("failed", "Codex 自升级失败，未自动回滚: "+err.Error())
		return
	}

	ctx, cancel = context.WithTimeout(s.app.Context(), 30*time.Second)
	afterProbe, probeErr := manager.Probe(ctx)
	cancel()
	installedVersion := previousVersion
	if probeErr == nil {
		installedVersion = textutil.FirstNonEmpty(afterProbe.CurrentVersion, installedVersion)
	}
	if strings.TrimSpace(installedVersion) != "" {
		appbackend.NewMaintenanceStateService(s.app).UpdateCodexUpgrade(func(snapshot *appbackend.BackendUpgradeSnapshot) {
			snapshot.TargetVersion = installedVersion
			snapshot.LatestVersion = installedVersion
		})
	}
	if probeErr != nil {
		finalize("failed", "Codex 自升级后版本检查失败，未自动回滚: "+probeErr.Error())
		return
	}

	update("smoke_testing", "正在验证 Codex runtime")
	ctx, cancel = context.WithTimeout(s.app.Context(), 45*time.Second)
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
		finalize("success", "Codex 自升级成功，已切换到 `"+textutil.FirstNonEmpty(installedVersion, targetVersion)+"`")
		return
	}
	finalize("success", "Codex 自升级成功，已验证 `"+textutil.FirstNonEmpty(installedVersion, targetVersion)+"` 可用；当前 frontend 未启用 Codex backend")
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
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return newUpgradeRenderService(s.app).renderRestartOperationCard(upgraderender.CodexSpec, sessionKey, snapshot)
		},
		func(message string) {
			appbackend.NewMaintenanceStateService(s.app).FinishCodexRestart("failed", message)
		},
	)
}

func (s backendUpgradeService) beginCodexRestartOperation() (appbackend.BackendRestartSnapshot, error) {
	if err := appbackend.NewMaintenanceStateService(s.app).EnsureCodexUpgradeReady(); err != nil {
		return appbackend.BackendRestartSnapshot{}, err
	}
	snapshot := appbackend.BackendRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: textutil.FirstNonEmpty(appbackend.NewMaintenanceStateService(s.app).CodexUpgradeState().CurrentVersion, appbackend.NewMaintenanceStateService(s.app).CodexRestartState().CurrentVersion),
	}
	if !appbackend.NewMaintenanceStateService(s.app).BeginCodexRestart(snapshot) {
		return appbackend.BackendRestartSnapshot{}, appbackend.ErrString("Codex 正在维护中，请稍后再试")
	}
	return appbackend.NewMaintenanceStateService(s.app).CodexRestartState(), nil
}

func (s backendUpgradeService) runCodexRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"codex restart progress patch failed",
		func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return newUpgradeRenderService(s.app).renderRestartOperationCard(upgraderender.CodexSpec, sessionKey, snapshot)
		},
		appbackend.NewMaintenanceStateService(s.app).UpdateCodexRestart,
		appbackend.NewMaintenanceStateService(s.app).FinishCodexRestart,
		func(snapshot *appbackend.BackendRestartSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)

	ctx, cancel := context.WithTimeout(s.app.Context(), 30*time.Second)
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	appbackend.NewMaintenanceStateService(s.app).UpdateCodexRestart(func(snapshot *appbackend.BackendRestartSnapshot) {
		snapshot.CurrentVersion = textutil.FirstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := appbackend.NewMaintenanceStateService(s.app).CodexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在准备新的 Codex runtime")
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(s.app.Context(), 45*time.Second)
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
