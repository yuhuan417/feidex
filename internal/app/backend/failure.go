package backend

import (
	"context"
	"strings"
	"time"

	appsessionctx "feidex/internal/app/sessionctx"
	appturnlifecycle "feidex/internal/app/turnlifecycle"
	appturnstream "feidex/internal/app/turnstream"
	"feidex/internal/state"
)

// BackendFailureService handles backend failure logic: iterating sessions,
// failing submissions, and resolving pending requests. All dependencies on
// the host app are injected as callback function fields.
type BackendFailureService struct {
	App App

	// ---- state access callbacks ----

	// AllSessions returns all sessions (after frontend filtering by the host).
	AllSessions func() []*state.Session

	// GetSubmission returns a submission by ID.
	GetSubmission func(id string) *state.Submission

	// AllPendingRequests returns all pending requests.
	AllPendingRequests func() []*state.PendingRequest

	// UpdatePending applies a mutation to a pending request.
	UpdatePending func(id string, mutate func(*state.PendingRequest)) error

	// FinalizeSubmission marks a submission as finalized.
	FinalizeSubmission func(id, status string) error

	// UpdateSession applies a mutation to a session.
	UpdateSession func(key string, mutate func(*state.Session)) (*state.Session, error)

	// ---- session helpers ----

	// SessionBelongsToFrontend reports whether a session belongs to this frontend.
	SessionBelongsToFrontend func(sessionKey string) bool

	// ---- turn / submission callbacks ----

	// RecordTurnError records an error for a turn.
	RecordTurnError func(threadID, turnID, message string)

	// FlushTurnStream flushes the turn stream and returns a flush result.
	FlushTurnStream func(ctx context.Context, threadID, turnID string) appturnstream.FlushResult

	// FailStandaloneCompactTurn fails a standalone compact turn.
	// Returns true if the error was handled.
	FailStandaloneCompactTurn func(threadID, turnID, message string) bool

	// BackendRuntimeFailsStandaloneCompaction reports whether the given
	// backend runtime fails standalone compaction.
	BackendRuntimeFailsStandaloneCompaction func(backend string) bool

	// BackendRuntimeHandleTransportFailure delegates transport failure
	// handling to the backend runtime.
	BackendRuntimeHandleTransportFailure func(backend, sessionKey, threadID string, err error)

	// ---- card / retry callbacks ----

	// ObserveAutoRetryTerminal observes an auto-retry terminal state.
	// Returns true if the terminal card should be suppressed.
	ObserveAutoRetryTerminal func(sessionKey, threadID, status string, sess *state.Session, sub *state.Submission, reuseMessageID string) bool

	// ReplaceTurnEventCard replaces the turn event card.
	ReplaceTurnEventCard func(ctx context.Context, sub *state.Submission, title, color, body, eventType, threadID, reuseMessageID string)

	// PrependAttentionMention prepends an attention mention to text.
	PrependAttentionMention func(text, userID string) string

	// TurnStopAttentionUserID returns the user ID for turn stop attention.
	TurnStopAttentionUserID func(sub *state.Submission, turnID string) string

	// CleanupSubmissionRuntimeState cleans up runtime state for a submission.
	CleanupSubmissionRuntimeState func(sub *state.Submission)

	// StartNextSubmissionAsync starts the next submission asynchronously.
	StartNextSubmissionAsync func(sessionKey, reason string)

	// RunAsync runs a function asynchronously.
	RunAsync func(fn func())
}

// NewBackendFailureService creates a new service.
func NewBackendFailureService(app App) BackendFailureService {
	return BackendFailureService{App: app}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// ErrorText returns the trimmed error text, or "" if err is nil.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// BackendFailureScopeMatches reports whether a session matches the given
// failure scope (session key and thread ID).
func BackendFailureScopeMatches(sess *state.Session, scopeSessionKey, scopeThreadID string) bool {
	if sess == nil {
		return false
	}
	scopeSessionKey = strings.TrimSpace(scopeSessionKey)
	scopeThreadID = strings.TrimSpace(scopeThreadID)
	if scopeSessionKey != "" && strings.TrimSpace(sess.Key) != scopeSessionKey {
		return false
	}
	if scopeThreadID != "" {
		if strings.TrimSpace(sess.ActiveThreadID) == scopeThreadID {
			return true
		}
		for _, op := range sess.ActiveOperations {
			if strings.TrimSpace(op.ThreadID) == scopeThreadID {
				return true
			}
		}
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Exported methods
// ---------------------------------------------------------------------------

// FailClaudeSessionActiveWork delegates Claude session failure to the backend
// runtime handle.
func (s BackendFailureService) FailClaudeSessionActiveWork(sessionKey, threadID string, err error) {
	if s.BackendRuntimeHandleTransportFailure != nil {
		s.BackendRuntimeHandleTransportFailure("claude", sessionKey, threadID, err)
	}
}

// FailBackendActiveWork iterates all sessions belonging to this frontend,
// finds active operations matching the failure scope, and fails them.
func (s BackendFailureService) FailBackendActiveWork(backend, scopeSessionKey, scopeThreadID, message string) {
	if s.AllSessions == nil {
		return
	}
	sessions := s.AllSessions()
	seenSubmissions := map[string]struct{}{}
	type compactTarget struct {
		threadID string
		turnID   string
	}
	compactTargets := make([]compactTarget, 0)
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if s.SessionBelongsToFrontend != nil && !s.SessionBelongsToFrontend(sess.Key) {
			continue
		}
		if !BackendFailureScopeMatches(sess, scopeSessionKey, scopeThreadID) {
			continue
		}
		appsessionctx.EnsureActiveOperations(sess)
		if len(sess.ActiveOperations) == 0 && strings.TrimSpace(sess.Status) != sessionStatusCompacting {
			continue
		}
		for _, op := range sess.ActiveOperations {
			if submissionID := strings.TrimSpace(op.SubmissionID); submissionID != "" {
				if _, ok := seenSubmissions[submissionID]; ok {
					continue
				}
				var sub *state.Submission
				if s.GetSubmission != nil {
					sub = s.GetSubmission(submissionID)
				}
				if sub == nil {
					continue
				}
				seenSubmissions[submissionID] = struct{}{}
				s.FailSubmissionWithoutTerminalCompletion(sess.Key, sub, strings.TrimSpace(op.ThreadID), strings.TrimSpace(op.TurnID), message)
				continue
			}
			if s.BackendRuntimeFailsStandaloneCompaction != nil && !s.BackendRuntimeFailsStandaloneCompaction(backend) {
				continue
			}
			threadID := firstNonEmpty(strings.TrimSpace(op.ThreadID), strings.TrimSpace(sess.ActiveThreadID))
			if threadID == "" {
				continue
			}
			compactTargets = append(compactTargets, compactTarget{
				threadID: threadID,
				turnID:   strings.TrimSpace(op.TurnID),
			})
		}
		if s.BackendRuntimeFailsStandaloneCompaction != nil && s.BackendRuntimeFailsStandaloneCompaction(backend) && strings.TrimSpace(sess.Status) == sessionStatusCompacting {
			threadID := strings.TrimSpace(sess.ActiveThreadID)
			if threadID != "" {
				compactTargets = append(compactTargets, compactTarget{threadID: threadID})
			}
		}
	}
	seenCompacts := map[string]struct{}{}
	for _, target := range compactTargets {
		key := target.threadID + "|" + target.turnID
		if _, ok := seenCompacts[key]; ok {
			continue
		}
		seenCompacts[key] = struct{}{}
		if s.FailStandaloneCompactTurn != nil {
			s.FailStandaloneCompactTurn(target.threadID, target.turnID, message)
		}
	}
}

// ResolvePendingRequestsForTerminalFailure resolves pending requests that
// match the given session/thread/turn scope.
func (s BackendFailureService) ResolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID string) {
	if s.AllPendingRequests == nil || s.UpdatePending == nil {
		return
	}
	now := time.Now().Unix()
	for _, req := range s.AllPendingRequests() {
		if req == nil || !isPendingRequestOpen(req) {
			continue
		}
		if sessionKey != "" && strings.TrimSpace(req.SessionKey) != strings.TrimSpace(sessionKey) {
			continue
		}
		if turnID != "" {
			if strings.TrimSpace(req.TurnID) != strings.TrimSpace(turnID) {
				continue
			}
		} else if threadID != "" && strings.TrimSpace(req.ThreadID) != strings.TrimSpace(threadID) {
			continue
		}
		_ = s.UpdatePending(req.ID, func(current *state.PendingRequest) {
			current.Status = "resolved"
			if current.ExpiresAt < now {
				return
			}
			current.ExpiresAt = now
		})
	}
}

// FailSubmissionWithoutTerminalCompletion fails a submission without waiting
// for a terminal completion event.
func (s BackendFailureService) FailSubmissionWithoutTerminalCompletion(sessionKey string, sub *state.Submission, threadID, turnID, message string) {
	if sub == nil || s.GetSubmission == nil || s.FinalizeSubmission == nil || s.UpdateSession == nil {
		return
	}
	current := s.GetSubmission(sub.ID)
	if current == nil || current.Finalized {
		return
	}
	sub = current
	threadID = firstNonEmpty(strings.TrimSpace(threadID), strings.TrimSpace(sub.ThreadID))
	turnID = firstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	if turnID != "" && message != "" && s.RecordTurnError != nil {
		s.RecordTurnError(threadID, turnID, message)
	}
	flush := appturnstream.FlushResult{}
	if turnID != "" && s.FlushTurnStream != nil {
		flush = s.FlushTurnStream(context.Background(), threadID, turnID)
	}
	s.ResolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID)
	_ = s.FinalizeSubmission(sub.ID, "failed")
	sub = s.GetSubmission(sub.ID)
	if sub == nil {
		return
	}
	terminalText := appturnlifecycle.TurnCompletionTerminalText(sub.Status, firstNonEmpty(strings.TrimSpace(message), strings.TrimSpace(flush.LastError)))
	reuseMessageID := strings.TrimSpace(flush.WorkingMessageID)
	updatedSess, _ := s.UpdateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		appsessionctx.RemoveActiveOperation(sess, sub.ID, turnID)
		switch {
		case appsessionctx.HasActiveOperations(sess):
			sess.Status = "turn_starting"
			for _, op := range sess.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					sess.Status = "turn_in_progress"
					break
				}
			}
		case len(sess.Queue) > 0 || len(sess.StagedImages) > 0:
			sess.Status = "queued"
		default:
			sess.Status = "idle"
		}
	})
	suppressTerminalCard := false
	if updatedSess != nil && s.ObserveAutoRetryTerminal != nil {
		suppressTerminalCard = s.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", updatedSess, sub, reuseMessageID)
	}
	if terminalText != "" && !suppressTerminalCard && s.ReplaceTurnEventCard != nil {
		attentionUserID := ""
		if s.TurnStopAttentionUserID != nil {
			attentionUserID = s.TurnStopAttentionUserID(sub, turnID)
		}
		body := terminalText
		if s.PrependAttentionMention != nil {
			body = s.PrependAttentionMention(body, attentionUserID)
		}
		s.ReplaceTurnEventCard(
			context.Background(),
			sub,
			"任务状态",
			"grey",
			body,
			"turn_terminal",
			"",
			reuseMessageID,
		)
	}
	if s.CleanupSubmissionRuntimeState != nil {
		s.CleanupSubmissionRuntimeState(sub)
	}
	if updatedSess != nil && sessionShouldStartNextSubmissionAsync(updatedSess) && s.StartNextSubmissionAsync != nil && s.RunAsync != nil {
		s.RunAsync(func() {
			s.StartNextSubmissionAsync(sessionKey, "backendFailed")
		})
	}
}

// isPendingRequestOpen checks if a pending request is still open.
func isPendingRequestOpen(req *state.PendingRequest) bool {
	if req == nil {
		return false
	}
	switch strings.TrimSpace(req.Status) {
	case "", "pending", "replied":
		return true
	default:
		return false
	}
}

// sessionShouldStartNextSubmissionAsync reports whether the session should
// start the next submission asynchronously.
func sessionShouldStartNextSubmissionAsync(sess *state.Session) bool {
	return sess != nil && !appsessionctx.HasInFlightSubmission(sess) && len(sess.Queue) > 0
}
