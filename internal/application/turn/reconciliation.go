package turn

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/application/backendops"
	"feidex/internal/domain/conversation"
)

type ReconciliationGateway interface {
	Available() bool
	ReadThreadTurns(context.Context, string) (backendops.ThreadTurns, error)
}
type Reconciliation struct {
	Gateway  ReconciliationGateway
	Session  func(string) *conversation.Session
	SawFinal func(string) bool
	Finish   func(string, string, string)
	Context  func() context.Context
}

func (s Reconciliation) AfterFinal(key string, sess *conversation.Session) *conversation.Session {
	if sess == nil || !s.SawFinal(strings.TrimSpace(sess.ActiveTurnID)) {
		return sess
	}
	return s.Reconcile(key, sess)
}

func (s Reconciliation) Reconcile(key string, sess *conversation.Session) *conversation.Session {
	if sess == nil || !s.Gateway.Available() || !conversation.HasInFlightSubmission(sess) {
		return sess
	}
	threadID, turnID := strings.TrimSpace(sess.ActiveThreadID), strings.TrimSpace(sess.ActiveTurnID)
	if threadID == "" || turnID == "" {
		return sess
	}
	ctx, cancel := context.WithTimeout(s.Context(), 5*time.Second)
	defer cancel()
	result, err := s.Gateway.ReadThreadTurns(ctx, threadID)
	if err != nil {
		slog.Warn("terminal turn reconciliation skipped", "session_key", key, "thread_id", threadID, "turn_id", turnID, "error", err)
		return sess
	}
	for _, candidate := range result.Turns {
		if strings.TrimSpace(candidate.ID) != turnID {
			continue
		}
		switch strings.TrimSpace(candidate.Status) {
		case "completed", "failed", "interrupted":
			slog.Warn("reconciling missed turn completion", "session_key", key, "thread_id", threadID, "turn_id", turnID, "status", candidate.Status)
			s.Finish(threadID, turnID, candidate.Status)
			return s.Session(key)
		default:
			return sess
		}
	}
	return sess
}
