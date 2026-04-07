package app

import (
	"log/slog"

	"feidex/internal/state"
)

func logSessionState(event, sessionKey string, sess *state.Session) {
	if sess == nil {
		slog.Info(event,
			"session_key", sessionKey,
			"session_missing", true,
		)
		return
	}
	slog.Info(event,
		"session_key", sessionKey,
		"workspace_id", sess.WorkspaceID,
		"active_thread_id", sess.ActiveThreadID,
		"active_thread_workspace_id", sess.ActiveThreadWorkspaceID,
		"active_turn_id", sess.ActiveTurnID,
		"active_submission_id", sess.ActiveSubmissionID,
		"status", sess.Status,
		"queue_len", len(sess.Queue),
		"queue", sess.Queue,
	)
}
