package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func (a *App) runCodexUpgradeOperation(messageID, sessionKey string, payload codexUpgradePendingPayload) {
	manager := newCodexInstallManager(a.cfg.Codex.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"codex upgrade progress patch failed",
		a.renderCodexUpgradeOperationCard,
		newMaintenanceStateService(a).updateCodexUpgrade,
		newMaintenanceStateService(a).finishCodexUpgrade,
		func(snapshot *codexUpgradeSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)
	runMaintenanceUpgradeWorkflow(maintenanceUpgradeWorkflow{
		PackageName:    "@openai/codex",
		BackendName:    "Codex",
		CurrentVersion: payload.CurrentVersion,
		TargetVersion:  payload.TargetVersion,
		Probe: func(ctx context.Context) (maintenanceUpgradeProbe, error) {
			probe, err := manager.Probe(ctx)
			return maintenanceUpgradeProbe{Supported: probe.Supported, Reason: probe.Reason, CurrentVersion: probe.CurrentVersion}, err
		},
		InstallVersion:    manager.InstallVersion,
		SmokeTest:         a.codexSmokeTest,
		RefreshRuntime:    a.refreshCodexRuntimeAfterMaintenance,
		RuntimeBusyReason: newMaintenanceStateService(a).codexUpgradeRuntimeBusyReason,
		RecordVersions: func(previousVersion, targetVersion string) {
			newMaintenanceStateService(a).updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
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

func (a *App) codexSmokeTest(ctx context.Context) error {
	client, err := a.startVerifiedCodexClient(ctx)
	if err != nil {
		return err
	}
	return client.Close()
}

func (a *App) startVerifiedCodexClient(ctx context.Context) (codexClient, error) {
	client := newCodexClient(a.cfg.Codex)
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	a.configureCodexClientRuntime(client)
	if err := client.Start(ctx, a.cfg.Codex.ExperimentalAPI); err != nil {
		return nil, err
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{"limit": 1, "includeHidden": false}, &result); err != nil {
		_ = client.Close()
		return nil, err
	}
	if len(result.Data) == 0 {
		_ = client.Close()
		return nil, fmt.Errorf("model/list returned no visible models")
	}
	return client, nil
}

func (a *App) refreshCodexRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("app not initialized")
	}
	if runtime := backendRuntimeForKind(backendCodex); runtime == nil || !runtime.isActive(a) {
		return false, a.codexSmokeTest(ctx)
	}
	next, err := a.startVerifiedCodexClient(ctx)
	if err != nil {
		return false, err
	}
	old := a.currentCodexClient()
	if old != nil {
		if err := old.Close(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			_ = next.Close()
			return false, fmt.Errorf("切换 runtime 失败: %w", err)
		}
	}
	a.replaceCodexClient(next)
	return true, nil
}

func (a *App) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		a,
		msg,
		a.beginCodexRestartOperation,
		a.runCodexRestartOperation,
		a.renderCodexRestartOperationCard,
		func(message string) { newMaintenanceStateService(a).finishCodexRestart("failed", message) },
	)
}

func (a *App) beginCodexRestartOperation() (codexRestartSnapshot, error) {
	if err := newMaintenanceStateService(a).ensureCodexUpgradeReady(); err != nil {
		return codexRestartSnapshot{}, err
	}
	snapshot := codexRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(newMaintenanceStateService(a).codexUpgradeState().CurrentVersion, newMaintenanceStateService(a).codexRestartState().CurrentVersion),
	}
	if !newMaintenanceStateService(a).beginCodexRestart(snapshot) {
		return codexRestartSnapshot{}, errString("Codex 正在维护中，请稍后再试")
	}
	return newMaintenanceStateService(a).codexRestartState(), nil
}

func (a *App) runCodexRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"codex restart progress patch failed",
		a.renderCodexRestartOperationCard,
		newMaintenanceStateService(a).updateCodexRestart,
		newMaintenanceStateService(a).finishCodexRestart,
		func(snapshot *codexRestartSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	manager := newCodexInstallManager(a.cfg.Codex.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	newMaintenanceStateService(a).updateCodexRestart(func(snapshot *codexRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := newMaintenanceStateService(a).codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在准备新的 Codex runtime")
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := a.refreshCodexRuntimeAfterMaintenance(ctx)
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
