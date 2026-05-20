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

func (s backendUpgradeService) runClaudeUpgradeOperation(messageID, sessionKey string, payload claudeUpgradePendingPayload) {
	manager := newClaudeInstallManager(s.app.cfg.Claude.Command)
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"claude upgrade progress patch failed",
		newUpgradeRenderService(s.app).renderClaudeUpgradeOperationCard,
		newMaintenanceStateService(s.app).UpdateClaudeUpgrade,
		newMaintenanceStateService(s.app).FinishClaudeUpgrade,
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
		finalize("failed", "当前环境不支持 Claude 自升级: "+firstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previousVersion := firstNonEmpty(probe.CurrentVersion, payload.CurrentVersion)
	targetVersion := firstNonEmpty(payload.TargetVersion, "latest")
	updateCommand := firstNonEmpty(probe.UpdateCommand, payload.UpdateCommand, "update")
	newMaintenanceStateService(s.app).UpdateClaudeUpgrade(func(snapshot *backendUpgradeSnapshot) {
		snapshot.CurrentVersion = previousVersion
		snapshot.PreviousVersion = previousVersion
		snapshot.TargetVersion = targetVersion
		snapshot.LatestVersion = targetVersion
	})
	if reason := newMaintenanceStateService(s.app).ClaudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "升级前检查失败: "+reason)
		return
	}

	update("installing", "正在运行 Claude 自升级命令 `"+firstNonEmpty(probe.Command, "claude")+" "+updateCommand+"`")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = manager.InstallVersion(ctx, cliSelfUpdateInstallTarget)
	cancel()
	if err != nil {
		finalize("failed", "Claude 自升级失败，未自动回滚: "+err.Error())
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
		newMaintenanceStateService(s.app).UpdateClaudeUpgrade(func(snapshot *backendUpgradeSnapshot) {
			snapshot.TargetVersion = installedVersion
			snapshot.LatestVersion = installedVersion
		})
	}
	if probeErr != nil {
		finalize("failed", "Claude 自升级后版本检查失败，未自动回滚: "+probeErr.Error())
		return
	}

	update("smoke_testing", "正在验证 Claude runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := newBackendUpgradeService(s.app).refreshClaudeRuntimeAfterMaintenance(ctx)
	cancel()
	if err != nil {
		finalize("failed", "Claude 自升级后 runtime 验证失败，未自动回滚: "+err.Error())
		return
	}
	if strings.TrimSpace(installedVersion) != "" && strings.TrimSpace(installedVersion) == strings.TrimSpace(previousVersion) {
		if switched {
			finalize("success", "Claude 已是最新版本 `"+installedVersion+"`，runtime 已重新验证")
			return
		}
		finalize("success", "Claude 已是最新版本 `"+installedVersion+"`；当前 frontend 未启用 Claude backend")
		return
	}
	if switched {
		finalize("success", "Claude 自升级成功，已切换到 `"+firstNonEmpty(installedVersion, targetVersion)+"`")
		return
	}
	finalize("success", "Claude 自升级成功，已验证 `"+firstNonEmpty(installedVersion, targetVersion)+"` 可用；当前 frontend 未启用 Claude backend")
}

func (s backendUpgradeService) claudeSmokeTest(ctx context.Context) error {
	if s.app == nil || s.app.cfg == nil {
		return fmt.Errorf("claude app not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workdir := "."
	for _, ws := range s.app.cfg.Workspaces {
		if cwd := strings.TrimSpace(ws.Cwd); cwd != "" {
			workdir = cwd
			break
		}
	}
	opts := []claudecli.SessionOption{
		claudecli.WithCLIPath(firstNonEmpty(strings.TrimSpace(s.app.cfg.Claude.Command), "claude")),
		claudecli.WithWorkDir(workdir),
		claudecli.WithPermissionMode(claudePermissionModeValue(s.app.cfg.Claude.PermissionMode)),
		claudecli.WithEventBufferSize(16),
	}
	if s.app.cfg.Claude.DangerouslySkipPermissions {
		opts = append(opts, claudecli.WithDangerouslySkipPermissions())
	}
	if model := strings.TrimSpace(s.app.cfg.Claude.Model); model != "" {
		opts = append(opts, claudecli.WithModel(model))
	}
	if effort := strings.TrimSpace(s.app.cfg.Claude.Effort); effort != "" {
		opts = append(opts, claudecli.WithEffort(effort))
	}
	if s.app.cfg.Claude.DisablePlugins {
		opts = append(opts, claudecli.WithDisablePlugins())
	}
	if strings.TrimSpace(s.app.cfg.Claude.SystemPrompt) != "" {
		opts = append(opts, claudecli.WithSystemPrompt(strings.TrimSpace(s.app.cfg.Claude.SystemPrompt)))
	}
	if s.app.cfg.Claude.PermissionPromptToolStdio {
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

func (s backendUpgradeService) refreshClaudeRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if s.app == nil {
		return false, fmt.Errorf("claude app not initialized")
	}
	if err := runClaudeSmokeTest(s.app, ctx); err != nil {
		return false, err
	}
	if runtime := backendRuntimeForKind(backendClaude); runtime == nil || !runtime.isActive(s.app) {
		return false, nil
	}
	if s.app.claude == nil {
		s.app.claude = newClaudeCore(s.app, s.app.cfg.Claude)
		return true, nil
	}
	if err := s.app.claude.Close(); err != nil {
		return false, fmt.Errorf("切换 runtime 失败: %w", err)
	}
	return true, nil
}

func (s backendUpgradeService) startClaudeRestartFromMessage(msg *feishu.InboundMessage) error {
	return startMaintenanceRestartFromMessage(
		s.app,
		msg,
		newBackendUpgradeService(s.app).beginClaudeRestartOperation,
		newBackendUpgradeService(s.app).runClaudeRestartOperation,
		newUpgradeRenderService(s.app).renderClaudeRestartOperationCard,
		func(message string) { newMaintenanceStateService(s.app).FinishClaudeRestart("failed", message) },
	)
}

func (s backendUpgradeService) beginClaudeRestartOperation() (backendRestartSnapshot, error) {
	if err := newMaintenanceStateService(s.app).EnsureClaudeUpgradeReady(); err != nil {
		return backendRestartSnapshot{}, err
	}
	snapshot := backendRestartSnapshot{
		Running:        true,
		Phase:          "preflight",
		Message:        "正在校验重启前置条件",
		CurrentVersion: firstNonEmpty(newMaintenanceStateService(s.app).ClaudeUpgradeState().CurrentVersion, newMaintenanceStateService(s.app).ClaudeRestartState().CurrentVersion),
	}
	if !newMaintenanceStateService(s.app).BeginClaudeRestart(snapshot) {
		return backendRestartSnapshot{}, errString("Claude 正在维护中，请稍后再试")
	}
	return newMaintenanceStateService(s.app).ClaudeRestartState(), nil
}

func (s backendUpgradeService) runClaudeRestartOperation(messageID, sessionKey string) {
	_, update, finalize := maintenanceSnapshotLifecycle(
		s.app,
		messageID,
		sessionKey,
		"claude restart progress patch failed",
		newUpgradeRenderService(s.app).renderClaudeRestartOperationCard,
		newMaintenanceStateService(s.app).UpdateClaudeRestart,
		newMaintenanceStateService(s.app).FinishClaudeRestart,
		func(snapshot *backendRestartSnapshot, phase, message string) {
			snapshot.Phase = phase
			snapshot.Message = message
		},
	)

	update("restarting", "正在校验 Claude runtime 状态")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	manager := newClaudeInstallManager(s.app.cfg.Claude.Command)
	probe, err := manager.Probe(ctx)
	cancel()
	if err != nil {
		finalize("failed", "重启前检查失败: "+err.Error())
		return
	}
	newMaintenanceStateService(s.app).UpdateClaudeRestart(func(snapshot *backendRestartSnapshot) {
		snapshot.CurrentVersion = firstNonEmpty(probe.CurrentVersion, snapshot.CurrentVersion)
	})
	if reason := newMaintenanceStateService(s.app).ClaudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		finalize("failed", "重启前检查失败: "+reason)
		return
	}

	update("restarting", "正在准备新的 Claude runtime")
	update("smoke_testing", "正在验证重启后的 runtime")
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	switched, err := newBackendUpgradeService(s.app).refreshClaudeRuntimeAfterMaintenance(ctx)
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
