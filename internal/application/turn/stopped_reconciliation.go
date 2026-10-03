package turn

import (
	"feidex/internal/domain/conversation"
	"log/slog"
	"strings"
)

type StoppedReconciliation struct {
	Stopped func(string) bool
	Session func(string) *conversation.Session
	Finish  func(string, string, string)
}

func (s StoppedReconciliation) Reconcile(key string, sess *conversation.Session) *conversation.Session {
	if sess == nil || !conversation.HasInFlightSubmission(sess) {
		return sess
	}
	threadID, turnID := strings.TrimSpace(sess.ActiveThreadID), strings.TrimSpace(sess.ActiveTurnID)
	if threadID == "" || turnID == "" || !s.Stopped(key) {
		return sess
	}
	slog.Warn("reconciling missed Claude turn completion", "session_key", key, "thread_id", threadID, "turn_id", turnID)
	s.Finish(threadID, turnID, "completed")
	return s.Session(key)
}
