package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func isTerminalTurnStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "interrupted":
		return true
	default:
		return false
	}
}

func reconcileCompletedCodexTurnFromFinalOutput(a *App, sessionKey string, sess *state.Session) *state.Session {
	if a == nil || sess == nil {
		return sess
	}
	if runtime := backendRuntimeForKind(backendCodex); runtime == nil || !runtime.isActive(a) {
		return sess
	}
	client := currentCodexClient(a)
	if client == nil {
		return sess
	}
	if !sessionHasInFlightSubmission(sess) {
		return sess
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	turnID := strings.TrimSpace(sess.ActiveTurnID)
	if threadID == "" || turnID == "" || !newTurnStreamService(a).turnStreamSawFinal(turnID) {
		return sess
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result codexrpc.ThreadReadResult
	if err := client.Call(ctx, "thread/read", map[string]any{
		"threadId":     threadID,
		"includeTurns": true,
	}, &result); err != nil {
		slog.Warn("codex terminal turn reconciliation skipped",
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return sess
	}

	for _, turn := range result.Thread.Turns {
		if strings.TrimSpace(turn.ID) != turnID {
			continue
		}
		if !isTerminalTurnStatus(turn.Status) {
			return sess
		}
		slog.Warn("reconciling missed codex turn completion",
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", turn.Status,
		)
		finishTurn(a, threadID, turnID, turn.Status)
		return appState(a).session(sessionKey)
	}
	return sess
}
