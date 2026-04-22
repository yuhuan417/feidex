package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/feishu"
)

func (a *App) runClaudeUpgradeOperation(messageID, sessionKey string, payload claudeUpgradePendingPayload) {
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"claude upgrade progress patch failed",
		a.renderClaudeUpgradeOperationCard,
		a.updateClaudeUpgrade,
		a.finishClaudeUpgrade,
		func(snapshot *claudeUpgradeSnapshot, phase, message string) {
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
		if err := runClaudeSmokeTest(a, ctx); err != nil {
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
	a.updateClaudeUpgrade(func(snapshot *claudeUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = payload.TargetVersion
		snapshot.LatestVersion = payload.TargetVersion
	})
	if reason := a.claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}
	if strings.TrimSpace(previousVersion) == strings.TrimSpace(payload.TargetVersion) {
		finalize("success", "当前已经是最新稳定版 `"+payload.TargetVersion+"`")
		return
	}

	update("installing", "正在安装 @anthropic-ai/claude-code@"+payload.TargetVersion)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = manager.InstallVersion(ctx, payload.TargetVersion)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}

	update("smoke_testing", "正在验证新版本")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := a.refreshClaudeRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		rollback(previousVersion, err)
		return
	}
	if switched {
		finalize("success", "升级成功，已切换到 `"+payload.TargetVersion+"`")
		return
	}
	finalize("success", "升级成功，已验证 `"+payload.TargetVersion+"` 可用；当前 frontend 未启用 Claude backend")
}

func (a *App) claudeSmokeTest(ctx context.Context) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("claude app not initialized")
	}
	workdir := "."
	for _, ws := range a.cfg.Workspaces {
		if cwd := strings.TrimSpace(ws.Cwd); cwd != "" {
			workdir = cwd
			break
		}
	}
	opts := []claudecli.SessionOption{
		claudecli.WithCLIPath(firstNonEmpty(strings.TrimSpace(a.cfg.Claude.Command), "claude")),
		claudecli.WithWorkDir(workdir),
		claudecli.WithPermissionMode(claudePermissionModeValue(a.cfg.Claude.PermissionMode)),
		claudecli.WithEventBufferSize(16),
	}
	if a.cfg.Claude.DangerouslySkipPermissions {
		opts = append(opts, claudecli.WithDangerouslySkipPermissions())
	}
	if model := strings.TrimSpace(a.cfg.Claude.Model); model != "" {
		opts = append(opts, claudecli.WithModel(model))
	}
	if effort := strings.TrimSpace(a.cfg.Claude.Effort); effort != "" {
		opts = append(opts, claudecli.WithEffort(effort))
	}
	if a.cfg.Claude.DisablePlugins {
		opts = append(opts, claudecli.WithDisablePlugins())
	}
	if strings.TrimSpace(a.cfg.Claude.SystemPrompt) != "" {
		opts = append(opts, claudecli.WithSystemPrompt(strings.TrimSpace(a.cfg.Claude.SystemPrompt)))
	}
	if a.cfg.Claude.PermissionPromptToolStdio {
		opts = append(opts, claudecli.WithPermissionPromptToolStdio())
	}
	session := claudecli.NewSession(opts...)
	if err := session.Start(ctx); err != nil {
		return err
	}
	defer session.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-session.Events():
			if !ok {
				if err := session.ExitError(); err != nil {
					return err
				}
				return fmt.Errorf("claude session exited before ready")
			}
			switch value := event.(type) {
			case claudecli.ReadyEvent:
				return nil
			case claudecli.ErrorEvent:
				if value.Error != nil {
					return value.Error
				}
				return fmt.Errorf("claude session startup failed")
			}
		}
	}
}

func (a *App) refreshClaudeRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("claude app not initialized")
	}
	if err := runClaudeSmokeTest(a, ctx); err != nil {
		return false, err
	}
	if a.configuredBackend() != backendClaude {
		return false, nil
	}
	if a.claude == nil {
		a.claude = newClaudeCore(a, a.cfg.Claude)
		return true, nil
	}
	if err := a.claude.Close(); err != nil {
		return false, fmt.Errorf("切换 runtime 失败: %w", err)
	}
	return true, nil
}

func (a *App) startClaudeRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		a,
		msg,
		a.beginClaudeRestartOperation,
		a.runClaudeRestartOperation,
		a.renderClaudeRestartOperationCard,
		func(message string) { a.finishClaudeRestart("failed", message) },
	)
}

func (a *App) beginClaudeRestartOperation() (claudeRestartSnapshot, error) {
	if err := a.ensureClaudeUpgradeReady(); err != nil {
		return claudeRestartSnapshot{}, err
	}
	snapshot := claudeRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(a.claudeUpgradeState().CurrentVersion, a.claudeRestartState().CurrentVersion),
	}
	if !a.beginClaudeRestart(snapshot) {
		return claudeRestartSnapshot{}, errString("Claude 正在维护中，请稍后再试")
	}
	return a.claudeRestartState(), nil
}

func (a *App) runClaudeRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"claude restart progress patch failed",
		a.renderClaudeRestartOperationCard,
		a.updateClaudeRestart,
		a.finishClaudeRestart,
		func(snapshot *claudeRestartSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)

	update("restarting", "正在校验 Claude runtime 状态")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	a.updateClaudeRestart(func(snapshot *claudeRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := a.claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在准备新的 Claude runtime")
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := a.refreshClaudeRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		finalize("failed", "Claude runtime 重启失败: "+err.Error())
		return
	}
	if switched {
		finalize("success", "Claude runtime 已原地重启，后续任务会使用新进程")
		return
	}
	finalize("success", "Claude CLI 校验通过；当前 frontend 未启用 Claude backend")
}
