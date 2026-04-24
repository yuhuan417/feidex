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

func (s backendUpgradeService) runCodexUpgradeOperation(messageID, sessionKey string, payload codexUpgradePendingPayload) {
	manager := newCodexInstallManager(s.app.cfg.Codex.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"codex upgrade progress patch failed",
		newUpgradeRenderService(s.app).renderCodexUpgradeOperationCard,
		newMaintenanceStateService(s.app).updateCodexUpgrade,
		newMaintenanceStateService(s.app).finishCodexUpgrade,
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
		SmokeTest:         newBackendUpgradeService(s.app).codexSmokeTest,
		RefreshRuntime:    newBackendUpgradeService(s.app).refreshCodexRuntimeAfterMaintenance,
		RuntimeBusyReason: newMaintenanceStateService(s.app).codexUpgradeRuntimeBusyReason,
		RecordVersions: func(previousVersion, targetVersion string) {
			newMaintenanceStateService(s.app).updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
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
	client, err := newBackendUpgradeService(s.app).startVerifiedCodexClient(ctx)
	if err != nil {
		return err
	}
	return client.Close()
}

func (s backendUpgradeService) startVerifiedCodexClient(ctx context.Context) (codexClient, error) {
	client := newCodexClient(s.app.cfg.Codex)
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	s.app.configureCodexClientRuntime(client)
	if err := client.Start(ctx, s.app.cfg.Codex.ExperimentalAPI); err != nil {
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

func (s backendUpgradeService) refreshCodexRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if s.app == nil {
		return false, fmt.Errorf("app not initialized")
	}
	if runtime := backendRuntimeForKind(backendCodex); runtime == nil || !runtime.isActive(s.app) {
		return false, newBackendUpgradeService(s.app).codexSmokeTest(ctx)
	}
	next, err := newBackendUpgradeService(s.app).startVerifiedCodexClient(ctx)
	if err != nil {
		return false, err
	}
	old := s.app.currentCodexClient()
	if old != nil {
		if err := old.Close(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			_ = next.Close()
			return false, fmt.Errorf("切换 runtime 失败: %w", err)
		}
	}
	s.app.replaceCodexClient(next)
	return true, nil
}

func (s backendUpgradeService) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.app,
		msg,
		newBackendUpgradeService(s.app).beginCodexRestartOperation,
		newBackendUpgradeService(s.app).runCodexRestartOperation,
		newUpgradeRenderService(s.app).renderCodexRestartOperationCard,
		func(message string) { newMaintenanceStateService(s.app).finishCodexRestart("failed", message) },
	)
}

func (s backendUpgradeService) beginCodexRestartOperation() (codexRestartSnapshot, error) {
	if err := newMaintenanceStateService(s.app).ensureCodexUpgradeReady(); err != nil {
		return codexRestartSnapshot{}, err
	}
	snapshot := codexRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(newMaintenanceStateService(s.app).codexUpgradeState().CurrentVersion, newMaintenanceStateService(s.app).codexRestartState().CurrentVersion),
	}
	if !newMaintenanceStateService(s.app).beginCodexRestart(snapshot) {
		return codexRestartSnapshot{}, errString("Codex 正在维护中，请稍后再试")
	}
	return newMaintenanceStateService(s.app).codexRestartState(), nil
}

func (s backendUpgradeService) runCodexRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"codex restart progress patch failed",
		newUpgradeRenderService(s.app).renderCodexRestartOperationCard,
		newMaintenanceStateService(s.app).updateCodexRestart,
		newMaintenanceStateService(s.app).finishCodexRestart,
		func(snapshot *codexRestartSnapshot, phase, message string) {
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
	newMaintenanceStateService(s.app).updateCodexRestart(func(snapshot *codexRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := newMaintenanceStateService(s.app).codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
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
