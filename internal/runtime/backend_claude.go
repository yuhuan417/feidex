package runtime

import (
	domainbackend "feidex/internal/domain/backend"

	"context"
	"feidex/internal/domain/conversation"
	"fmt"
	"log/slog"
	"strings"
)

type claudeRuntimeFacade struct{}

func (claudeRuntimeFacade) Kind() string { return domainbackend.BackendClaude }

func (claudeRuntimeFacade) DisplayName() string { return "Claude" }

func (claudeRuntimeFacade) ConfiguredCommand(ctx BackendContext) string {
	if ctx.Cfg == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Cfg.Claude.Command)
}

func (claudeRuntimeFacade) IsActive(ctx BackendContext) bool {
	return ctx.Backend == domainbackend.BackendClaude
}

func (claudeRuntimeFacade) RuntimeReady(ctx BackendContext) bool {
	return ctx.Claude != nil
}

func (claudeRuntimeFacade) BeginStartupRecoveryScope(BackendContext) func() {
	return func() {}
}

func (claudeRuntimeFacade) ReconcileCompletedTurnFromFinalOutput(ctx BackendContext, sessionKey string, sess *conversation.Session) *conversation.Session {
	if ctx.ReconcileClaudeCompletedTurn != nil {
		return ctx.ReconcileClaudeCompletedTurn(sessionKey, sess)
	}
	return sess
}

func (claudeRuntimeFacade) ClearActiveOperationsAfterInterruptContext(ctx BackendContext, sessionKey string, sess *conversation.Session) *conversation.Session {
	if ctx.ClearActiveOperations == nil {
		return sess
	}
	return ctx.ClearActiveOperations(sessionKey, sess)
}

func (claudeRuntimeFacade) BuildRuntime(ctx BackendContext) *BackendHandle {
	if ctx.NewClaudeCore == nil {
		return &BackendHandle{Backend: domainbackend.BackendClaude}
	}
	return &BackendHandle{
		Backend: domainbackend.BackendClaude,
		Claude:  ctx.NewClaudeCore(),
	}
}

func (claudeRuntimeFacade) StartRuntime(context.Context, BackendContext, *BackendHandle) error {
	return nil
}

func (claudeRuntimeFacade) MaintenanceActive(ctx BackendContext) bool {
	return ctx.ClaudeMaintenanceActive != nil && ctx.ClaudeMaintenanceActive()
}

func (claudeRuntimeFacade) MaintenanceBlocksCommand(ctx BackendContext, raw string) error {
	if ctx.MaintenanceBlocksCommand == nil {
		return nil
	}
	return ctx.MaintenanceBlocksCommand(raw)
}

func (claudeRuntimeFacade) IdleMaintenanceBlockedReason() string {
	return "当前正在执行 Claude 维护，请稍后再切换 backend"
}

func (claudeRuntimeFacade) ResolvesPendingLocally(string) bool {
	return true
}

func (claudeRuntimeFacade) DeferQueuedSubmissionsDuringRecovery(BackendContext) bool {
	return false
}

func (claudeRuntimeFacade) DropThreadLineageAfterStartFailure(BackendContext, error) bool {
	return false
}

func (claudeRuntimeFacade) FailsStandaloneCompaction() bool {
	return false
}

func (claudeRuntimeFacade) HandleTransportFailure(ctx BackendContext, sessionKey, threadID string, err error) {
	if ctx.HandleTransportFailure == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" && threadID == "" {
		return
	}
	message := "Claude 会话异常结束。"
	if detail := strings.TrimSpace(backendErrorText(err)); detail != "" {
		message = "Claude 会话异常结束：" + detail
	}
	slog.Warn("claude session failed",
		"session_key", sessionKey,
		"thread_id", threadID,
		"error", err,
	)
	ctx.HandleTransportFailure(sessionKey, threadID, fmt.Errorf("%s", message))
}
