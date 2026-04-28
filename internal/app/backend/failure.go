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
// failing submissions, and resolving pending requests.
type BackendFailureService struct {
	App  App
	deps FailureDeps
}

type FailureStateDeps struct {
	AllSessions        func() []*state.Session
	GetSubmission      func(id string) *state.Submission
	AllPendingRequests func() []*state.PendingRequest
	UpdatePending      func(id string, mutate func(*state.PendingRequest)) error
	FinalizeSubmission func(id, status string) error
	UpdateSession      func(key string, mutate func(*state.Session)) (*state.Session, error)
}

type FailureSessionDeps struct {
	SessionBelongsToFrontend func(sessionKey string) bool
}

type FailureRuntimeDeps struct {
	RecordTurnError                         func(threadID, turnID, message string)
	FlushTurnStream                         func(ctx context.Context, threadID, turnID string) appturnstream.FlushResult
	FailStandaloneCompactTurn               func(threadID, turnID, message string) bool
	BackendRuntimeFailsStandaloneCompaction func(backend string) bool
	BackendRuntimeHandleTransportFailure    func(backend, sessionKey, threadID string, err error)
}

type FailureCardDeps struct {
	ObserveAutoRetryTerminal func(sessionKey, threadID, status string, sess *state.Session, sub *state.Submission, reuseMessageID string) bool
	ReplaceTurnEventCard     func(ctx context.Context, sub *state.Submission, title, color, body, eventType, threadID, reuseMessageID string)
	PrependAttentionMention  func(text, userID string) string
	TurnStopAttentionUserID  func(sub *state.Submission, turnID string) string
}

type FailureAsyncDeps struct {
	CleanupSubmissionRuntimeState func(sub *state.Submission)
	StartNextSubmissionAsync      func(sessionKey, reason string)
	RunAsync                      func(fn func())
}

type FailureDeps struct {
	App      App
	State    FailureStateDeps
	Sessions FailureSessionDeps
	Runtime  FailureRuntimeDeps
	Cards    FailureCardDeps
	Async    FailureAsyncDeps
}

// NewBackendFailureService creates a new service.
func NewBackendFailureService(deps FailureDeps) BackendFailureService {
	return BackendFailureService{App: deps.App, deps: deps}
}

func (s BackendFailureService) AllSessions() []*state.Session {
	if s.deps.State.AllSessions == nil {
		return nil
	}
	return s.deps.State.AllSessions()
}

func (s BackendFailureService) GetSubmission(id string) *state.Submission {
	if s.deps.State.GetSubmission == nil {
		return nil
	}
	return s.deps.State.GetSubmission(id)
}

func (s BackendFailureService) AllPendingRequests() []*state.PendingRequest {
	if s.deps.State.AllPendingRequests == nil {
		return nil
	}
	return s.deps.State.AllPendingRequests()
}

func (s BackendFailureService) UpdatePending(id string, mutate func(*state.PendingRequest)) error {
	if s.deps.State.UpdatePending == nil {
		return nil
	}
	return s.deps.State.UpdatePending(id, mutate)
}

func (s BackendFailureService) FinalizeSubmission(id, status string) error {
	if s.deps.State.FinalizeSubmission == nil {
		return nil
	}
	return s.deps.State.FinalizeSubmission(id, status)
}

func (s BackendFailureService) UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	if s.deps.State.UpdateSession == nil {
		return nil, nil
	}
	return s.deps.State.UpdateSession(key, mutate)
}

func (s BackendFailureService) SessionBelongsToFrontend(sessionKey string) bool {
	if s.deps.Sessions.SessionBelongsToFrontend == nil {
		return true
	}
	return s.deps.Sessions.SessionBelongsToFrontend(sessionKey)
}

func (s BackendFailureService) RecordTurnError(threadID, turnID, message string) {
	if s.deps.Runtime.RecordTurnError != nil {
		s.deps.Runtime.RecordTurnError(threadID, turnID, message)
	}
}

func (s BackendFailureService) FlushTurnStream(ctx context.Context, threadID, turnID string) appturnstream.FlushResult {
	if s.deps.Runtime.FlushTurnStream == nil {
		return appturnstream.FlushResult{}
	}
	return s.deps.Runtime.FlushTurnStream(ctx, threadID, turnID)
}

func (s BackendFailureService) FailStandaloneCompactTurn(threadID, turnID, message string) bool {
	if s.deps.Runtime.FailStandaloneCompactTurn == nil {
		return false
	}
	return s.deps.Runtime.FailStandaloneCompactTurn(threadID, turnID, message)
}

func (s BackendFailureService) BackendRuntimeFailsStandaloneCompaction(backend string) bool {
	if s.deps.Runtime.BackendRuntimeFailsStandaloneCompaction == nil {
		return false
	}
	return s.deps.Runtime.BackendRuntimeFailsStandaloneCompaction(backend)
}

func (s BackendFailureService) BackendRuntimeHandleTransportFailure(backend, sessionKey, threadID string, err error) {
	if s.deps.Runtime.BackendRuntimeHandleTransportFailure != nil {
		s.deps.Runtime.BackendRuntimeHandleTransportFailure(backend, sessionKey, threadID, err)
	}
}

func (s BackendFailureService) ObserveAutoRetryTerminal(sessionKey, threadID, status string, sess *state.Session, sub *state.Submission, reuseMessageID string) bool {
	if s.deps.Cards.ObserveAutoRetryTerminal == nil {
		return false
	}
	return s.deps.Cards.ObserveAutoRetryTerminal(sessionKey, threadID, status, sess, sub, reuseMessageID)
}

func (s BackendFailureService) ReplaceTurnEventCard(ctx context.Context, sub *state.Submission, title, color, body, eventType, threadID, reuseMessageID string) {
	if s.deps.Cards.ReplaceTurnEventCard != nil {
		s.deps.Cards.ReplaceTurnEventCard(ctx, sub, title, color, body, eventType, threadID, reuseMessageID)
	}
}

func (s BackendFailureService) PrependAttentionMention(text, userID string) string {
	if s.deps.Cards.PrependAttentionMention == nil {
		return text
	}
	return s.deps.Cards.PrependAttentionMention(text, userID)
}

func (s BackendFailureService) TurnStopAttentionUserID(sub *state.Submission, turnID string) string {
	if s.deps.Cards.TurnStopAttentionUserID == nil {
		return ""
	}
	return s.deps.Cards.TurnStopAttentionUserID(sub, turnID)
}

func (s BackendFailureService) CleanupSubmissionRuntimeState(sub *state.Submission) {
	if s.deps.Async.CleanupSubmissionRuntimeState != nil {
		s.deps.Async.CleanupSubmissionRuntimeState(sub)
	}
}

func (s BackendFailureService) StartNextSubmissionAsync(sessionKey, reason string) {
	if s.deps.Async.StartNextSubmissionAsync != nil {
		s.deps.Async.StartNextSubmissionAsync(sessionKey, reason)
	}
}

func (s BackendFailureService) RunAsync(fn func()) {
	if s.deps.Async.RunAsync != nil {
		s.deps.Async.RunAsync(fn)
	}
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
	s.BackendRuntimeHandleTransportFailure("claude", sessionKey, threadID, err)
}

// FailBackendActiveWork iterates all sessions belonging to this frontend,
// finds active operations matching the failure scope, and fails them.
func (s BackendFailureService) FailBackendActiveWork(backend, scopeSessionKey, scopeThreadID, message string) {
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
		if !s.SessionBelongsToFrontend(sess.Key) {
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
				sub := s.GetSubmission(submissionID)
				if sub == nil {
					continue
				}
				seenSubmissions[submissionID] = struct{}{}
				s.FailSubmissionWithoutTerminalCompletion(sess.Key, sub, strings.TrimSpace(op.ThreadID), strings.TrimSpace(op.TurnID), message)
				continue
			}
			if !s.BackendRuntimeFailsStandaloneCompaction(backend) {
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
		if s.BackendRuntimeFailsStandaloneCompaction(backend) && strings.TrimSpace(sess.Status) == sessionStatusCompacting {
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
		s.FailStandaloneCompactTurn(target.threadID, target.turnID, message)
	}
}

// ResolvePendingRequestsForTerminalFailure resolves pending requests that
// match the given session/thread/turn scope.
func (s BackendFailureService) ResolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID string) {
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
	if sub == nil {
		return
	}
	current := s.GetSubmission(sub.ID)
	if current == nil || current.Finalized {
		return
	}
	sub = current
	threadID = firstNonEmpty(strings.TrimSpace(threadID), strings.TrimSpace(sub.ThreadID))
	turnID = firstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	if turnID != "" && message != "" {
		s.RecordTurnError(threadID, turnID, message)
	}
	flush := appturnstream.FlushResult{}
	if turnID != "" {
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
	if updatedSess != nil {
		suppressTerminalCard = s.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", updatedSess, sub, reuseMessageID)
	}
	if terminalText != "" && !suppressTerminalCard {
		attentionUserID := s.TurnStopAttentionUserID(sub, turnID)
		body := s.PrependAttentionMention(terminalText, attentionUserID)
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
	s.CleanupSubmissionRuntimeState(sub)
	if updatedSess != nil && sessionShouldStartNextSubmissionAsync(updatedSess) {
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
