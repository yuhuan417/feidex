// Package compaction owns standalone context compaction state and lifecycle.
package compaction

import (
	"context"
	"feidex/internal/domain/conversation"
	apputil "feidex/internal/formatutil"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"
)

type SessionStore interface {
	GetSession(key string) *conversation.Session
	AllSessions() []*conversation.Session
	SaveSession(sess *conversation.Session) error
}

type Gateway interface {
	StartCompaction(context.Context, string) error
}
type Dependencies struct {
	Context    func() context.Context
	Repository SessionStore
	Gateway    Gateway
	Notices    func(context.Context, *conversation.Session, string)
}

type Service struct{ Deps Dependencies }

func (s Service) context() context.Context {
	if s.Deps.Context != nil {
		return s.Deps.Context()
	}
	return context.Background()
}

var SessionHasActiveWork = conversation.HasActiveWork

func IsContextCompactionItem(item map[string]any) bool {
	t := strings.ToLower(strings.TrimSpace(apputil.StringValue(item["type"])))
	t = strings.ReplaceAll(t, "-", "_")
	return t == "context_compaction" || t == "contextcompaction"
}
func normalizeWorkingStatus(v any) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(apputil.StringValue(v))), "_", "")
}

// StartThreadCompaction starts a context compaction on the active thread.
func (s Service) StartThreadCompaction(sessionKey string) (*conversation.Session, error) {
	if s.Deps.Repository == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	store := s.Deps.Repository
	if store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	sess := store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法压缩上下文")
	}
	if SessionHasActiveWork(sess) {
		return nil, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	previousStatus := strings.TrimSpace(sess.Status)
	sess.Status = conversation.SessionStatusCompacting.String()
	if err := store.SaveSession(sess); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.context(), 20*time.Second)
	defer cancel()
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if s.Deps.Gateway == nil {
		RestoreSession(store, sessionKey, threadID, previousStatus)
		return nil, fmt.Errorf("codex client not initialized")
	}
	if err := s.Deps.Gateway.StartCompaction(ctx, threadID); err != nil {
		RestoreSession(store, sessionKey, threadID, previousStatus)
		return nil, err
	}
	return store.GetSession(sessionKey), nil
}

// BindStandaloneCompactTurn binds a compact turn to a session.
func (s Service) BindStandaloneCompactTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if s.Deps.Repository == nil || threadID == "" || turnID == "" {
		return false
	}
	store := s.Deps.Repository
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if currentTurn := conversation.FindActiveOperationByTurn(sess, turnID); currentTurn != nil && strings.TrimSpace(currentTurn.SubmissionID) == "" {
			return true
		}
		if conversation.HasInFlightSubmission(sess) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if currentTurn := conversation.ForegroundOperation(sess); currentTurn != nil && strings.TrimSpace(currentTurn.TurnID) != "" && strings.TrimSpace(currentTurn.TurnID) != turnID {
			continue
		}
		if conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusCompacting {
			continue
		}
		conversation.UpsertActiveOperation(sess, conversation.SessionActiveOperation{
			Kind:     conversation.OpKindTurn,
			ThreadID: threadID,
			TurnID:   turnID,
		})
		sess.Status = conversation.SessionStatusCompacting.String()
		return store.SaveSession(sess) == nil
	}
	return false
}

// NoteStandaloneCompactItemStarted records that a compact item has started.
func (s Service) NoteStandaloneCompactItemStarted(threadID, turnID string, item map[string]any) bool {
	if !IsContextCompactionItem(item) {
		return false
	}
	return s.BindStandaloneCompactTurn(threadID, turnID)
}

// CompleteStandaloneCompactTurn completes a standalone compact turn.
func (s Service) CompleteStandaloneCompactTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if s.Deps.Repository == nil || threadID == "" {
		return false
	}
	store := s.Deps.Repository
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if op := conversation.FindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if turnID != "" && conversation.FindActiveOperationByTurn(sess, turnID) == nil && conversation.HasActiveOperations(sess) {
			continue
		}
		if conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusCompacting && conversation.FindActiveOperationByThread(sess, threadID) == nil {
			continue
		}
		resolvedTurnID := strings.TrimSpace(turnID)
		if resolvedTurnID == "" {
			if op := conversation.FindActiveOperationByThread(sess, threadID); op != nil && strings.TrimSpace(op.SubmissionID) == "" {
				resolvedTurnID = strings.TrimSpace(op.TurnID)
			}
		}
		conversation.RemoveActiveOperation(sess, "", resolvedTurnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = conversation.SessionStatusQueued.String()
		} else {
			sess.Status = conversation.SessionStatusIdle.String()
		}
		if err := store.SaveSession(sess); err != nil {
			return false
		}
		s.sendStandaloneCompactResult(sess, "completed")
		return true
	}
	return false
}

// CompleteStandaloneCompactItem completes a standalone compact item if it is a
// context compaction item.
func (s Service) CompleteStandaloneCompactItem(threadID, turnID string, item map[string]any) bool {
	if !IsContextCompactionItem(item) {
		return false
	}
	switch normalizeWorkingStatus(textutil.FirstNonEmpty(apputil.StringValue(item["status"]), apputil.StringValue(item["state"]))) {
	case "", "completed":
		return s.CompleteStandaloneCompactTurn(threadID, turnID)
	case "interrupted", "cancelled", "canceled":
		return s.FinishStandaloneCompactTurn(threadID, turnID, "interrupted")
	default:
		return false
	}
}

// FinishStandaloneCompactTurn finishes a standalone compact turn with the
// given status.
func (s Service) FinishStandaloneCompactTurn(threadID, turnID, status string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if s.Deps.Repository == nil || threadID == "" || turnID == "" {
		return false
	}
	store := s.Deps.Repository
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if op := conversation.FindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if conversation.FindActiveOperationByTurn(sess, turnID) == nil {
			continue
		}
		conversation.RemoveActiveOperation(sess, "", turnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = conversation.SessionStatusQueued.String()
		} else {
			sess.Status = conversation.SessionStatusIdle.String()
		}
		if err := store.SaveSession(sess); err != nil {
			return false
		}
		s.sendStandaloneCompactResult(sess, status)
		return true
	}
	return false
}

// FailStandaloneCompactTurn fails a standalone compact turn with the given
// message.
func (s Service) FailStandaloneCompactTurn(threadID, turnID, message string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	message = strings.TrimSpace(message)
	if s.Deps.Repository == nil || threadID == "" {
		return false
	}
	store := s.Deps.Repository
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if op := conversation.FindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if turnID != "" && conversation.FindActiveOperationByTurn(sess, turnID) == nil && conversation.HasActiveOperations(sess) {
			continue
		}
		if conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusCompacting && conversation.FindActiveOperationByThread(sess, threadID) == nil {
			continue
		}
		resolvedTurnID := strings.TrimSpace(turnID)
		if resolvedTurnID == "" {
			if op := conversation.FindActiveOperationByThread(sess, threadID); op != nil && strings.TrimSpace(op.SubmissionID) == "" {
				resolvedTurnID = strings.TrimSpace(op.TurnID)
			}
		}
		conversation.RemoveActiveOperation(sess, "", resolvedTurnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = conversation.SessionStatusQueued.String()
		} else {
			sess.Status = conversation.SessionStatusIdle.String()
		}
		if err := store.SaveSession(sess); err != nil {
			return false
		}
		text := "当前线程上下文压缩失败。"
		if message != "" {
			text = "当前线程上下文压缩失败：" + message
		}
		s.SendSessionTextNotice(sess, text)
		return true
	}
	return false
}

// RestoreSession restores a session to its previous status after a failed
// compaction start. It is a standalone helper that does not require a Service.
func RestoreSession(store SessionStore, sessionKey, threadID, previousStatus string) {
	if store == nil {
		return
	}
	sess := store.GetSession(sessionKey)
	if sess == nil {
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return
	}
	if conversation.HasActiveOperations(sess) {
		return
	}
	if conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusCompacting {
		return
	}
	sess.Status = strings.TrimSpace(previousStatus)
	if sess.Status == "" {
		sess.Status = conversation.SessionStatusIdle.String()
	}
	_ = store.SaveSession(sess)
}

// RestoreStandaloneCompactSession restores a session to its previous status
// after a failed compaction start.
func (s Service) RestoreStandaloneCompactSession(sessionKey, threadID, previousStatus string) {
	if s.Deps.Repository == nil {
		return
	}
	store := s.Deps.Repository
	RestoreSession(store, sessionKey, threadID, previousStatus)
}

// StandaloneCompactResultText returns the user-facing text for a compact result
// status.
func StandaloneCompactResultText(status string) string {
	switch strings.TrimSpace(status) {
	case "completed":
		return "当前线程上下文已压缩完成。"
	case "interrupted":
		return "当前线程上下文压缩已中断。"
	case "failed":
		return "当前线程上下文压缩失败。"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s Service) sendStandaloneCompactResult(sess *conversation.Session, status string) {
	text := StandaloneCompactResultText(status)
	if text == "" {
		return
	}
	s.SendSessionTextNotice(sess, text)
}

// SendSessionTextNotice sends a text notice to the session's chat.
func (s Service) SendSessionTextNotice(sess *conversation.Session, text string) {
	if s.Deps.Notices == nil || sess == nil || strings.TrimSpace(text) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(s.context(), 10*time.Second)
	defer cancel()
	s.Deps.Notices(ctx, sess, strings.TrimSpace(text))
}
