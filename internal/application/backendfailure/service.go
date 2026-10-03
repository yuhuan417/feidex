package backendfailure

import (
	"context"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appturnlifecycle "feidex/internal/application/turn"

	"feidex/internal/domain/interaction"
	"feidex/internal/textutil"
)

// BackendFailureService handles backend failure logic: iterating sessions,
// failing submissions, and resolving pending requests.
type BackendFailureService struct {
	deps FailureDeps
}

type FailureStateDeps struct {
	AllSessions        func() []*conversation.Session
	GetSubmission      func(id string) *domainsubmission.Submission
	AllPendingRequests func() []*interaction.PendingRequest
	GetSession         func(string) *conversation.Session
	CommitTerminal     func(*conversation.Session, *conversation.Session, *domainsubmission.Submission, *domainsubmission.Submission, []*interaction.PendingRequest, []*interaction.PendingRequest) error
}

type FailureSessionDeps struct {
	SessionBelongsToFrontend func(sessionKey string) bool
}

type FailureRuntimeDeps struct {
	RecordTurnError                         func(threadID, turnID, message string)
	FlushTurnStream                         func(ctx context.Context, threadID, turnID string) appturnlifecycle.StreamSummary
	FailStandaloneCompactTurn               func(threadID, turnID, message string) bool
	BackendRuntimeFailsStandaloneCompaction func(backend string) bool
}

type FailureCardDeps struct {
	ExpireClaudeInteractions func(string)
	ObserveAutoRetryTerminal func(sessionKey, threadID, status string, sess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool
	ReplaceTurnEventCard     func(ctx context.Context, sub *domainsubmission.Submission, title, color, body, eventType, threadID, reuseMessageID string)
	PrependAttentionMention  func(text, userID string) string
	TurnStopAttentionUserID  func(sub *domainsubmission.Submission, turnID string) string
}

type FailureAsyncDeps struct {
	CleanupSubmissionRuntimeState      func(sub *domainsubmission.Submission)
	ClearSubmissionProcessingReactions func(sub *domainsubmission.Submission)
	StartNextSubmissionAsync           func(sessionKey, reason string)
	NextQueuedSubmissionSessionKey     func(sessionKey string) string
	RunAsync                           func(fn func())
	// RunSessionAsync serializes queue continuation with the target session.
	RunSessionAsync func(sessionKey string, fn func())
}

type FailureDeps struct {
	Context  func() context.Context
	State    FailureStateDeps
	Sessions FailureSessionDeps
	Runtime  FailureRuntimeDeps
	Cards    FailureCardDeps
	Async    FailureAsyncDeps
}

// NewBackendFailureService creates a new service.
func NewBackendFailureService(deps FailureDeps) BackendFailureService {
	return BackendFailureService{deps: deps}
}

func (s BackendFailureService) AllSessions() []*conversation.Session {
	if s.deps.State.AllSessions == nil {
		return nil
	}
	return s.deps.State.AllSessions()
}

func (s BackendFailureService) GetSubmission(id string) *domainsubmission.Submission {
	if s.deps.State.GetSubmission == nil {
		return nil
	}
	return s.deps.State.GetSubmission(id)
}

func (s BackendFailureService) AllPendingRequests() []*interaction.PendingRequest {
	if s.deps.State.AllPendingRequests == nil {
		return nil
	}
	return s.deps.State.AllPendingRequests()
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

func (s BackendFailureService) FlushTurnStream(ctx context.Context, threadID, turnID string) appturnlifecycle.StreamSummary {
	if s.deps.Runtime.FlushTurnStream == nil {
		return appturnlifecycle.StreamSummary{}
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

func (s BackendFailureService) ObserveAutoRetryTerminal(sessionKey, threadID, status string, sess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool {
	if s.deps.Cards.ObserveAutoRetryTerminal == nil {
		return false
	}
	return s.deps.Cards.ObserveAutoRetryTerminal(sessionKey, threadID, status, sess, sub, reuseMessageID, lastError)
}

func (s BackendFailureService) ReplaceTurnEventCard(ctx context.Context, sub *domainsubmission.Submission, title, color, body, eventType, threadID, reuseMessageID string) {
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

func (s BackendFailureService) TurnStopAttentionUserID(sub *domainsubmission.Submission, turnID string) string {
	if s.deps.Cards.TurnStopAttentionUserID == nil {
		return ""
	}
	return s.deps.Cards.TurnStopAttentionUserID(sub, turnID)
}

func (s BackendFailureService) CleanupSubmissionRuntimeState(sub *domainsubmission.Submission) {
	if s.deps.Async.CleanupSubmissionRuntimeState != nil {
		s.deps.Async.CleanupSubmissionRuntimeState(sub)
	}
}

func (s BackendFailureService) ClearSubmissionProcessingReactions(sub *domainsubmission.Submission) {
	if s.deps.Async.ClearSubmissionProcessingReactions != nil {
		s.deps.Async.ClearSubmissionProcessingReactions(sub)
	}
}

func (s BackendFailureService) StartNextSubmissionAsync(sessionKey, reason string) {
	if s.deps.Async.StartNextSubmissionAsync != nil {
		s.deps.Async.StartNextSubmissionAsync(sessionKey, reason)
	}
}

func (s BackendFailureService) NextQueuedSubmissionSessionKey(sessionKey string) string {
	if s.deps.Async.NextQueuedSubmissionSessionKey != nil {
		return strings.TrimSpace(s.deps.Async.NextQueuedSubmissionSessionKey(sessionKey))
	}
	return ""
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
func BackendFailureScopeMatches(sess *conversation.Session, scopeSessionKey, scopeThreadID string) bool {
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
	if strings.TrimSpace(sessionKey) == "" && strings.TrimSpace(threadID) == "" {
		return
	}
	if s.deps.Cards.ExpireClaudeInteractions != nil {
		s.deps.Cards.ExpireClaudeInteractions(sessionKey)
	}
	message := "Claude 会话异常结束。"
	if detail := ErrorText(err); detail != "" {
		message = fmt.Sprintf("Claude 会话异常结束：%s", detail)
	}
	s.FailBackendActiveWork("claude", sessionKey, threadID, message)
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
		conversation.EnsureActiveOperations(sess)
		if len(sess.ActiveOperations) == 0 && conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusCompacting {
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
			threadID := textutil.FirstNonEmpty(strings.TrimSpace(op.ThreadID), strings.TrimSpace(sess.ActiveThreadID))
			if threadID == "" {
				continue
			}
			compactTargets = append(compactTargets, compactTarget{
				threadID: threadID,
				turnID:   strings.TrimSpace(op.TurnID),
			})
		}
		if s.BackendRuntimeFailsStandaloneCompaction(backend) && conversation.NormalizeSessionStatus(sess.Status) == conversation.SessionStatusCompacting {
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

// FailSubmissionWithoutTerminalCompletion fails a submission without waiting
// for a terminal completion event.
func (s BackendFailureService) FailSubmissionWithoutTerminalCompletion(sessionKey string, sub *domainsubmission.Submission, threadID, turnID, message string) {
	if sub == nil {
		return
	}
	current := s.GetSubmission(sub.ID)
	if current == nil || current.Finalized {
		return
	}
	sub = current
	threadID = textutil.FirstNonEmpty(strings.TrimSpace(threadID), strings.TrimSpace(sub.ThreadID))
	turnID = textutil.FirstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	expectedSession := s.deps.State.GetSession(sessionKey)
	if expectedSession == nil {
		return
	}
	updatedSess := conversation.CloneSession(expectedSession)
	conversation.RemoveActiveOperation(updatedSess, sub.ID, turnID)
	switch {
	case conversation.HasActiveOperations(updatedSess):
		updatedSess.Status = conversation.SessionStatusTurnStarting.String()
		for _, op := range updatedSess.ActiveOperations {
			if strings.TrimSpace(op.TurnID) != "" {
				updatedSess.Status = conversation.SessionStatusTurnInProgress.String()
				break
			}
		}
	case len(updatedSess.Queue) > 0 || len(updatedSess.StagedImages) > 0:
		updatedSess.Status = conversation.SessionStatusQueued.String()
	default:
		updatedSess.Status = conversation.SessionStatusIdle.String()
	}
	nextSubmission := *sub
	nextSubmission.Finalize(domainsubmission.SubmissionStatusFailed.String())
	var expectedRequests, nextRequests []*interaction.PendingRequest
	now := time.Now().Unix()
	for _, req := range s.AllPendingRequests() {
		if req == nil || req.SessionKey != sessionKey {
			continue
		}
		expectedRequests = append(expectedRequests, req)
		if !isPendingRequestOpen(req) {
			continue
		}
		if turnID != "" && req.TurnID != turnID {
			continue
		}
		if turnID == "" && threadID != "" && req.ThreadID != threadID {
			continue
		}
		cp := *req
		cp.Status = "resolved"
		if cp.ExpiresAt > now {
			cp.ExpiresAt = now
		}
		nextRequests = append(nextRequests, &cp)
	}
	if err := s.deps.State.CommitTerminal(expectedSession, updatedSess, sub, &nextSubmission, expectedRequests, nextRequests); err != nil {
		slog.Error("backend failure state commit failed", "session_key", sessionKey, "submission_id", sub.ID, "error", err)
		return
	}
	sub = &nextSubmission
	if turnID != "" && message != "" {
		s.RecordTurnError(threadID, turnID, message)
	}
	flush := appturnlifecycle.StreamSummary{}
	if turnID != "" {
		flush = s.FlushTurnStream(s.context(), threadID, turnID)
	}
	s.ClearSubmissionProcessingReactions(sub)
	terminalText := appturnlifecycle.TurnCompletionTerminalText(sub.Status, textutil.FirstNonEmpty(strings.TrimSpace(message), strings.TrimSpace(flush.LastError)))
	reuseMessageID := strings.TrimSpace(flush.WorkingMessageID)
	suppressTerminalCard := false
	if updatedSess != nil {
		suppressTerminalCard = s.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", updatedSess, sub, reuseMessageID, textutil.FirstNonEmpty(strings.TrimSpace(message), strings.TrimSpace(flush.LastError)))
	}
	if terminalText != "" && !suppressTerminalCard {
		attentionUserID := s.TurnStopAttentionUserID(sub, turnID)
		body := s.PrependAttentionMention(terminalText, attentionUserID)
		s.ReplaceTurnEventCard(
			s.context(),
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
	nextSessionKey := ""
	if updatedSess != nil {
		nextSessionKey = s.NextQueuedSubmissionSessionKey(sessionKey)
	}
	if nextSessionKey != "" {
		if s.deps.Async.RunSessionAsync != nil {
			s.deps.Async.RunSessionAsync(nextSessionKey, func() {
				s.StartNextSubmissionAsync(nextSessionKey, "backendFailed")
			})
		} else {
			s.RunAsync(func() {
				s.StartNextSubmissionAsync(nextSessionKey, "backendFailed")
			})
		}
	}
}

// isPendingRequestOpen checks if a pending request is still open.
func isPendingRequestOpen(req *interaction.PendingRequest) bool {
	if req == nil {
		return false
	}
	return interaction.IsPendingRequestOpen(req.Status)
}

func (s BackendFailureService) context() context.Context {
	if s.deps.Context != nil {
		return s.deps.Context()
	}
	return context.Background()
}
