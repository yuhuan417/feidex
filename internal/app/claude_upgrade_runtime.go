package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/feishu"
)

const claudeSmokeInitGracePeriod = time.Second

func (a *App) runClaudeUpgradeOperation(messageID, sessionKey string, payload claudeUpgradePendingPayload) {
	manager := newClaudeInstallManager(a.cfg.Claude.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"claude upgrade progress patch failed",
		newUpgradeRenderService(a).renderClaudeUpgradeOperationCard,
		newMaintenanceStateService(a).updateClaudeUpgrade,
		newMaintenanceStateService(a).finishClaudeUpgrade,
		func(snapshot *claudeUpgradeSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)
	runMaintenanceUpgradeWorkflow(maintenanceUpgradeWorkflow{
		PackageName:    "@anthropic-ai/claude-code",
		BackendName:    "Claude",
		CurrentVersion: payload.CurrentVersion,
		TargetVersion:  payload.TargetVersion,
		Probe: func(ctx context.Context) (maintenanceUpgradeProbe, error) {
			probe, err := manager.Probe(ctx)
			return maintenanceUpgradeProbe{Supported: probe.Supported, Reason: probe.Reason, CurrentVersion: probe.CurrentVersion}, err
		},
		InstallVersion:    manager.InstallVersion,
		SmokeTest:         func(ctx context.Context) error { return runClaudeSmokeTest(a, ctx) },
		RefreshRuntime:    a.refreshClaudeRuntimeAfterMaintenance,
		RuntimeBusyReason: newMaintenanceStateService(a).claudeUpgradeRuntimeBusyReason,
		RecordVersions: func(previousVersion, targetVersion string) {
			newMaintenanceStateService(a).updateClaudeUpgrade(func(snapshot *claudeUpgradeSnapshot) {
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

func (a *App) claudeSmokeTest(ctx context.Context) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("claude app not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
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
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	if err := session.Start(sessionCtx); err != nil {
		return err
	}
	defer session.Stop()
	if err := session.Initialize(ctx); err != nil {
		return err
	}
	return waitForClaudeSmokeStable(ctx, session, claudeSmokeInitGracePeriod)
}

func waitForClaudeSmokeStable(ctx context.Context, session *claudecli.Session, grace time.Duration) error {
	if session == nil {
		return fmt.Errorf("claude session not initialized")
	}
	if grace <= 0 {
		grace = claudeSmokeInitGracePeriod
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if err := session.ExitError(); err != nil {
				return err
			}
			return nil
		case event, ok := <-session.Events():
			if !ok {
				if err := session.ExitError(); err != nil {
					return err
				}
				return fmt.Errorf("claude session exited after initialize")
			}
			switch value := event.(type) {
			case claudecli.ReadyEvent:
				return nil
			case claudecli.ErrorEvent:
				return claudeSmokeEventError(session, value)
			}
		}
	}
}

func claudeSmokeEventError(session *claudecli.Session, event claudecli.ErrorEvent) error {
	if event.Error == nil {
		return fmt.Errorf("claude session startup failed after initialize")
	}
	if event.Context == "stdout_eof" || (event.Context == "read_line" && strings.Contains(strings.ToLower(event.Error.Error()), "file already closed")) {
		if session != nil {
			if err := session.ExitError(); err != nil {
				return fmt.Errorf("claude session exited after initialize: %w", err)
			}
		}
		return fmt.Errorf("claude session exited after initialize")
	}
	return event.Error
}

func (a *App) refreshClaudeRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("claude app not initialized")
	}
	if err := runClaudeSmokeTest(a, ctx); err != nil {
		return false, err
	}
	if runtime := backendRuntimeForKind(backendClaude); runtime == nil || !runtime.isActive(a) {
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
		newUpgradeRenderService(a).renderClaudeRestartOperationCard,
		func(message string) { newMaintenanceStateService(a).finishClaudeRestart("failed", message) },
	)
}

func (a *App) beginClaudeRestartOperation() (claudeRestartSnapshot, error) {
	if err := newMaintenanceStateService(a).ensureClaudeUpgradeReady(); err != nil {
		return claudeRestartSnapshot{}, err
	}
	snapshot := claudeRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(newMaintenanceStateService(a).claudeUpgradeState().CurrentVersion, newMaintenanceStateService(a).claudeRestartState().CurrentVersion),
	}
	if !newMaintenanceStateService(a).beginClaudeRestart(snapshot) {
		return claudeRestartSnapshot{}, errString("Claude 正在维护中，请稍后再试")
	}
	return newMaintenanceStateService(a).claudeRestartState(), nil
}

func (a *App) runClaudeRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		a,
		messageID,
		sessionKey,
		"claude restart progress patch failed",
		newUpgradeRenderService(a).renderClaudeRestartOperationCard,
		newMaintenanceStateService(a).updateClaudeRestart,
		newMaintenanceStateService(a).finishClaudeRestart,
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
	newMaintenanceStateService(a).updateClaudeRestart(func(snapshot *claudeRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := newMaintenanceStateService(a).claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
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
