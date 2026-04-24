package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/state"
)

type claudeRuntimeFacade struct{}

func (claudeRuntimeFacade) kind() string { return backendClaude }

func (claudeRuntimeFacade) displayName() string { return "Claude" }

func (claudeRuntimeFacade) configuredCommand(a *App) string {
	if a == nil || a.cfg == nil {
		return ""
	}
	return strings.TrimSpace(a.cfg.Claude.Command)
}

func (claudeRuntimeFacade) isActive(a *App) bool {
	return a != nil && a.configuredBackend() == backendClaude
}

func (claudeRuntimeFacade) runtimeReady(a *App) bool {
	return a != nil && a.claude != nil
}

func (claudeRuntimeFacade) beginStartupRecoveryScope(*App) func() {
	return func() {}
}

func (claudeRuntimeFacade) reconcileCompletedTurnFromFinalOutput(_ *App, _ string, sess *state.Session) *state.Session {
	return sess
}

func (claudeRuntimeFacade) conversationBackend(a *App) conversationBackendFacade {
	return claudeConversationBackend{app: a}
}

func (claudeRuntimeFacade) configuration(a *App) backendConfigurationFacade {
	return claudeBackendConfigurationFacade{app: a}
}

func (claudeRuntimeFacade) serverRequestAdapter(a *App) serverRequestBackendAdapter {
	return claudeServerRequestAdapter{app: a}
}

func (claudeRuntimeFacade) buildRuntime(a *App) *backendRuntimeHandle {
	if a == nil {
		return &backendRuntimeHandle{backend: backendClaude}
	}
	return &backendRuntimeHandle{
		backend: backendClaude,
		claude:  newClaudeCore(a, a.cfg.Claude),
	}
}

func (claudeRuntimeFacade) startRuntime(context.Context, *App, *backendRuntimeHandle) error {
	return nil
}

func (claudeRuntimeFacade) maintenanceActive(a *App) bool {
	return a != nil && a.claudeMaintenanceActive()
}

func (claudeRuntimeFacade) maintenanceBlocksCommand(a *App, raw string) error {
	if a == nil {
		return nil
	}
	return a.claudeMaintenanceBlocksCommand(raw)
}

func (claudeRuntimeFacade) idleMaintenanceBlockedReason() string {
	return "当前正在执行 Claude 维护，请稍后再切换 backend"
}

func (claudeRuntimeFacade) resolvesPendingLocally(string) bool {
	return true
}

func (claudeRuntimeFacade) deferQueuedSubmissionsDuringRecovery(*App) bool {
	return false
}

func (claudeRuntimeFacade) dropThreadLineageAfterStartFailure(*App, error) bool {
	return false
}

func (claudeRuntimeFacade) failsStandaloneCompaction() bool {
	return false
}

func (claudeRuntimeFacade) handleTransportFailure(a *App, sessionKey, threadID string, err error) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" && threadID == "" {
		return
	}
	message := "Claude 会话异常结束。"
	if detail := strings.TrimSpace(errorText(err)); detail != "" {
		message = "Claude 会话异常结束：" + detail
	}
	slog.Warn("claude session failed",
		"session_key", sessionKey,
		"thread_id", threadID,
		"error", err,
	)
	a.failBackendActiveWork(backendClaude, sessionKey, threadID, message)
}
