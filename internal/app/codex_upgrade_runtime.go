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
		a.updateCodexUpgrade,
		a.finishCodexUpgrade,
		func(snapshot *codexUpgradeSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)
	rollback := func(previousVersion string, cause error) {
		update("rolling_back", "升级失败，正在回滚到 "+firstNonEmpty(previousVersion, "-"))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if strings.TrimSpace(previousVersion) == "" {
			finalize("rollback_failed", "升级失败，且缺少可回滚的旧版本。原因: "+cause.Error())
			return
		}
		if err := manager.InstallVersion(ctx, previousVersion); err != nil {
			finalize("rollback_failed", "升级失败，自动回滚也失败。原始错误: "+cause.Error()+"；回滚错误: "+err.Error())
			return
		}
		if err := a.codexSmokeTest(ctx); err != nil {
			finalize("rollback_failed", "升级失败，回滚后的 smoke test 也失败。原始错误: "+cause.Error()+"；回滚验证错误: "+err.Error())
			return
		}
		finalize("rolled_back", "升级失败，已自动回滚到 `"+previousVersion+"`。原因: "+cause.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "升级前检查失败: "+err.Error())
		return
	}
	if !probe.Supported {
		finalize("failed", "当前环境不支持自动升级: "+firstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previousVersion := firstNonEmpty(probe.CurrentVersion, payload.CurrentVersion)
	a.updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = payload.TargetVersion
		snapshot.LatestVersion = payload.TargetVersion
	})
	if reason := a.codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}
	if strings.TrimSpace(previousVersion) == strings.TrimSpace(payload.TargetVersion) {
		finalize("success", "当前已经是最新稳定版 `"+payload.TargetVersion+"`")
		return
	}

	update("installing", "正在安装 @openai/codex@"+payload.TargetVersion)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = manager.InstallVersion(ctx, payload.TargetVersion)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}

	update("smoke_testing", "正在验证新版本")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := a.refreshCodexRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}
	if switched {
		finalize("success", "升级成功，已切换到 `"+payload.TargetVersion+"`")
		return
	}
	finalize("success", "升级成功，已验证 `"+payload.TargetVersion+"` 可用；当前 frontend 未启用 Codex backend")
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
	if a.configuredBackend() != backendCodex {
		return false, a.codexSmokeTest(ctx)
	}
	next, err := a.startVerifiedCodexClient(ctx)
	if err != nil {
		return false, err
	}
	old := a.codex
	if old != nil {
		if err := old.Close(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			_ = next.Close()
			return false, fmt.Errorf("切换 runtime 失败: %w", err)
		}
	}
	a.codex = next
	return true, nil
}

func (a *App) startCodexRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		a,
		msg,
		a.beginCodexRestartOperation,
		a.runCodexRestartOperation,
		a.renderCodexRestartOperationCard,
		func(message string) { a.finishCodexRestart("failed", message) },
	)
}

func (a *App) beginCodexRestartOperation() (codexRestartSnapshot, error) {
	if err := a.ensureCodexUpgradeReady(); err != nil {
		return codexRestartSnapshot{}, err
	}
	snapshot := codexRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(a.codexUpgradeState().CurrentVersion, a.codexRestartState().CurrentVersion),
	}
	if !a.beginCodexRestart(snapshot) {
		return codexRestartSnapshot{}, errString("Codex 正在维护中，请稍后再试")
	}
	return a.codexRestartState(), nil
}

func (a *App) runCodexRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"codex restart progress patch failed",
		a.renderCodexRestartOperationCard,
		a.updateCodexRestart,
		a.finishCodexRestart,
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
	a.updateCodexRestart(func(snapshot *codexRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := a.codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
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
