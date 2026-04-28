package app

import (
	"context"
	"log/slog"
	"strings"

	appconvbackend "feidex/internal/app/convbackend"
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
	return a != nil && configuredBackend(a) == backendClaude
}

func (claudeRuntimeFacade) runtimeReady(a *App) bool {
	return a != nil && a.claude != nil
}

func (claudeRuntimeFacade) beginStartupRecoveryScope(*App) func() {
	return func() {}
}

func (claudeRuntimeFacade) reconcileCompletedTurnFromFinalOutput(a *App, sessionKey string, sess *state.Session) *state.Session {
	if a == nil || sess == nil {
		return sess
	}
	if !sessionHasInFlightSubmission(sess) {
		return sess
	}
	turnID := strings.TrimSpace(sess.ActiveTurnID)
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if turnID == "" || threadID == "" {
		return sess
	}
	if a.claude == nil || !a.claude.SessionStopped(sessionKey) {
		return sess
	}
	slog.Warn("reconciling missed Claude turn completion",
		"session_key", sessionKey,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	finishTurn(a, threadID, turnID, "completed")
	return a.State().Session(sessionKey)
}

func (claudeRuntimeFacade) clearActiveOperationsAfterInterrupt(a *App, sessionKey string, sess *state.Session) *state.Session {
	if a == nil || sess == nil {
		return sess
	}
	if !sessionHasActiveOperations(sess) {
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
			if err := a.State().UpdateSubmission(subID, func(value *state.Submission) {
				value.Status = "interrupted"
				value.Finalized = true
			}); err != nil {
				slog.Error("clear active submission after interrupt failed", "submission_id", subID, "error", err)
			}
		}
	}
	updatedSess, err := a.State().UpdateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		sessionResetActiveOperations(current)
		current.Status = "idle"
	})
	if err != nil {
		slog.Error("clear active operations after interrupt failed", "session_key", sessionKey, "error", err)
		return sess
	}
	return updatedSess
}

func (claudeRuntimeFacade) conversationBackend(a *App) appconvbackend.ConversationBackendFacade {
	return appconvbackend.NewClaudeConversationBackend(a)
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
	return a != nil && newMaintenanceStateService(a).ClaudeMaintenanceActive()
}

func (claudeRuntimeFacade) maintenanceBlocksCommand(a *App, raw string) error {
	if a == nil {
		return nil
	}
	return newMaintenanceStateService(a).ClaudeMaintenanceBlocksCommand(raw)
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
	failBackendActiveWork(a, backendClaude, sessionKey, threadID, message)
}
