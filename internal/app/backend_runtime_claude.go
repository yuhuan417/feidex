package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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

func (claudeRuntimeFacade) runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	if a == nil {
		return nil
	}
	msg := a.commandMessageFromAction(action, sessionKey, "/compact")
	return a.enqueueSubmission(msg)
}

func (claudeRuntimeFacade) handleCompactCommand(a *App, msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	return a.enqueuePassthroughCommand(msg, "/compact")
}

func (claudeRuntimeFacade) completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := actionStringValue(action, "parent_action")
	return a.completeAsyncCommandAction(
		action,
		sessionKey,
		"/stop",
		parentAction,
		"正在请求中断当前任务",
		a.renderInterruptPreparingCard(sessionKey, parentAction),
		func(sessionKey, text string) map[string]any {
			return a.renderInterruptResultCard(sessionKey, parentAction, text)
		},
		func(sessionKey, errText string) map[string]any {
			return a.renderInterruptFailedCard(sessionKey, parentAction, targetTurnID, errText)
		},
		"interrupt patch failed",
	)
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
