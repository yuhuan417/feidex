package app

import (
	domainbackend "feidex/internal/domain/backend"
	domainsubmission "feidex/internal/domain/submission"

	"context"
	"feidex/internal/domain/conversation"
	"fmt"
	"log/slog"
	"strings"
)

type claudeRuntimeFacade struct{}

func (claudeRuntimeFacade) kind() string { return domainbackend.BackendClaude }

func (claudeRuntimeFacade) displayName() string { return "Claude" }

func (claudeRuntimeFacade) configuredCommand(ctx backendRuntimeContext) string {
	if ctx.cfg == nil {
		return ""
	}
	return strings.TrimSpace(ctx.cfg.Claude.Command)
}

func (claudeRuntimeFacade) isActive(ctx backendRuntimeContext) bool {
	return ctx.backend == domainbackend.BackendClaude
}

func (claudeRuntimeFacade) runtimeReady(ctx backendRuntimeContext) bool {
	return ctx.claude != nil
}

func (claudeRuntimeFacade) beginStartupRecoveryScope(backendRuntimeContext) func() {
	return func() {}
}

func (claudeRuntimeFacade) reconcileCompletedTurnFromFinalOutput(ctx backendRuntimeContext, sessionKey string, sess *conversation.Session) *conversation.Session {
	if ctx.claude == nil || sess == nil {
		return sess
	}
	if !conversation.HasInFlightSubmission(sess) {
		return sess
	}
	turnID := strings.TrimSpace(sess.ActiveTurnID)
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if turnID == "" || threadID == "" {
		return sess
	}
	if !ctx.claude.SessionStopped(sessionKey) {
		return sess
	}
	slog.Warn("reconciling missed Claude turn completion",
		"session_key", sessionKey,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	if ctx.reconcileClaudeCompletedTurn != nil {
		return ctx.reconcileClaudeCompletedTurn(sessionKey, sess)
	}
	return sess
}

func clearClaudeActiveOperationsAfterInterrupt(a *App, sessionKey string, sess *conversation.Session) *conversation.Session {
	if a == nil || sess == nil {
		return sess
	}
	if !conversation.HasActiveOperations(sess) {
		return sess
	}
	slog.Debug("clearing Claude active operations after interrupt",
		"session_key", sessionKey,
		"active_operations_count", len(sess.ActiveOperations),
	)
	// Finalize active submissions BEFORE updating the session to avoid
	// deadlock (updateSession holds the store lock).
	for _, op := range sess.ActiveOperations {
		subID := strings.TrimSpace(op.SubmissionID)
		if subID == "" {
			continue
		}
		if sub := a.State().Submission(subID); sub != nil && !sub.Finalized {
			if err := a.State().UpdateSubmission(subID, func(value *domainsubmission.Submission) {
				value.Status = domainsubmission.SubmissionStatusInterrupted.String()
				value.Finalized = true
			}); err != nil {
				slog.Error("clear active submission after interrupt failed", "submission_id", subID, "error", err)
			}
		}
	}
	updatedSess, err := a.State().UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		conversation.ResetActiveOperations(current)
		current.Status = conversation.SessionStatusIdle.String()
	})
	if err != nil {
		slog.Error("clear active operations after interrupt failed", "session_key", sessionKey, "error", err)
		return sess
	}
	return updatedSess
}

func (claudeRuntimeFacade) clearActiveOperationsAfterInterruptContext(ctx backendRuntimeContext, sessionKey string, sess *conversation.Session) *conversation.Session {
	if ctx.clearActiveOperations == nil {
		return sess
	}
	return ctx.clearActiveOperations(sessionKey, sess)
}

// clearActiveOperationsAfterInterrupt keeps the old in-package helper shape
// for legacy tests and callers; production bindings use the explicit runtime
// context method above.
func (claudeRuntimeFacade) clearActiveOperationsAfterInterrupt(a *App, sessionKey string, sess *conversation.Session) *conversation.Session {
	return clearClaudeActiveOperationsAfterInterrupt(a, sessionKey, sess)
}

func (claudeRuntimeFacade) buildRuntime(ctx backendRuntimeContext) *backendRuntimeHandle {
	if ctx.newClaudeCore == nil {
		return &backendRuntimeHandle{backend: domainbackend.BackendClaude}
	}
	return &backendRuntimeHandle{
		backend: domainbackend.BackendClaude,
		claude:  ctx.newClaudeCore(),
	}
}

func (claudeRuntimeFacade) startRuntime(context.Context, backendRuntimeContext, *backendRuntimeHandle) error {
	return nil
}

func (claudeRuntimeFacade) maintenanceActive(ctx backendRuntimeContext) bool {
	return ctx.claudeMaintenanceActive != nil && ctx.claudeMaintenanceActive()
}

func (claudeRuntimeFacade) maintenanceBlocksCommand(ctx backendRuntimeContext, raw string) error {
	if ctx.maintenanceBlocksCommand == nil {
		return nil
	}
	return ctx.maintenanceBlocksCommand(raw)
}

func (claudeRuntimeFacade) idleMaintenanceBlockedReason() string {
	return "当前正在执行 Claude 维护，请稍后再切换 backend"
}

func (claudeRuntimeFacade) resolvesPendingLocally(string) bool {
	return true
}

func (claudeRuntimeFacade) deferQueuedSubmissionsDuringRecovery(backendRuntimeContext) bool {
	return false
}

func (claudeRuntimeFacade) dropThreadLineageAfterStartFailure(backendRuntimeContext, error) bool {
	return false
}

func (claudeRuntimeFacade) failsStandaloneCompaction() bool {
	return false
}

func (claudeRuntimeFacade) handleTransportFailure(ctx backendRuntimeContext, sessionKey, threadID string, err error) {
	if ctx.handleTransportFailure == nil {
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
	ctx.handleTransportFailure(sessionKey, threadID, fmt.Errorf("%s", message))
}
