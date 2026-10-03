// Package goal owns active goals and backend-driven continuation binding.
package goal

import (
	"context"
	"strings"
	"time"

	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
)

const (
	SubmissionKind        = "goal"
	ContinuationInputText = "[goal continuation]"
)

type Repository interface {
	Session(string) *conversation.Session
	Sessions() []*conversation.Session
	CreateSubmission(*domainsubmission.Submission) (string, error)
	UpdateSession(string, func(*conversation.Session)) (*conversation.Session, error)
	DeleteSubmission(string)
}
type AnchorPresenter interface {
	SendContinuationAnchor(context.Context, string, conversation.ThreadGoal, int) (string, error)
}
type Bindings interface {
	BindTurnSubmission(string, string, string, string)
	MarkTurnStartedAt(string, time.Time)
}
type Replies interface {
	RecordSubmissionSourceLinks(*domainsubmission.Submission)
	RecordRootTurnBinding(string, string, string, string)
}
type Streams interface {
	NoteTurnStarted(string, *domainsubmission.Submission)
}
type LiveThreads interface{ MarkSessionThreadLive(string, string) }
type Dependencies struct {
	Context            func() context.Context
	Repository         Repository
	Tracker            *Tracker
	Presenter          AnchorPresenter
	Bindings           Bindings
	Replies            Replies
	Streams            Streams
	Live               LiveThreads
	DefaultWorkspaceID func() string
	BelongsToFrontend  func(string) bool
}
type Service struct{ Deps Dependencies }

func (s Service) BindGoalContinuationTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return false
	}
	goal, ok := s.Deps.Tracker.ActiveGoal(threadID)
	if !ok {
		return false
	}
	sessionKey, sess := s.findGoalContinuationSession(threadID)
	if sess == nil || sessionKey == "" {
		return false
	}
	if conversation.HasActiveOperations(sess) {
		return false
	}
	anchor, ok := s.sendGoalContinuationAnchor(sessionKey, threadID, turnID, sess, goal)
	if !ok {
		return false
	}
	triggerMessageID := anchor.MessageID
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), s.Deps.DefaultWorkspaceID())
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          workspaceID,
		ThreadID:             threadID,
		TurnID:               turnID,
		UserID:               strings.TrimSpace(sess.OwnerUserID),
		ChatID:               strings.TrimSpace(anchor.ChatID),
		TriggerMessageID:     triggerMessageID,
		SourceMessageIDs:     uniqueStrings([]string{triggerMessageID}),
		SourceRootMessageIDs: uniqueStrings([]string{triggerMessageID}),
		InputText:            ContinuationInputText,
		Kind:                 SubmissionKind,
		Status:               domainsubmission.SubmissionStatusRunning.String(),
	}
	id, err := s.Deps.Repository.CreateSubmission(sub)
	if err != nil || strings.TrimSpace(id) == "" {
		return false
	}
	sub.ID = id
	updatedSess, err := s.Deps.Repository.UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		conversation.UpsertActiveOperation(current, conversation.SessionActiveOperation{
			Kind:         conversation.OpKindSubmission,
			SubmissionID: id,
			ThreadID:     threadID,
			TurnID:       turnID,
		})
		current.Status = conversation.SessionStatusTurnInProgress.String()
		conversation.SetThreadContext(current, workspaceID, threadID, current.ActiveThreadName, current.ActiveThreadPreview)
	})
	if err != nil || updatedSess == nil {
		s.Deps.Repository.DeleteSubmission(id)
		return false
	}
	s.Deps.Bindings.BindTurnSubmission(threadID, turnID, sessionKey, id)
	s.Deps.Bindings.MarkTurnStartedAt(turnID, time.Now())
	s.Deps.Replies.RecordSubmissionSourceLinks(sub)
	s.Deps.Replies.RecordRootTurnBinding(triggerMessageID, sessionKey, threadID, turnID)
	s.Deps.Streams.NoteTurnStarted(sessionKey, sub)
	s.Deps.Live.MarkSessionThreadLive(sessionKey, threadID)
	return true
}

func (s Service) sendGoalContinuationAnchor(sessionKey, threadID, turnID string, sess *conversation.Session, goal conversation.ThreadGoal) (Anchor, bool) {
	if s.Deps.Repository == nil || s.Deps.Presenter == nil || sess == nil {
		return Anchor{}, false
	}
	anchor := Anchor{
		SessionKey: sessionKey,
		ThreadID:   threadID,
		ChatID:     strings.TrimSpace(sess.ChatID),
		ChatType:   strings.TrimSpace(sess.ChatType),
		UserID:     strings.TrimSpace(sess.OwnerUserID),
	}
	if recorded, ok := s.Deps.Tracker.Anchor(threadID); ok {
		anchor.ChatID = textutil.FirstNonEmpty(anchor.ChatID, strings.TrimSpace(recorded.ChatID))
		anchor.ChatType = textutil.FirstNonEmpty(anchor.ChatType, strings.TrimSpace(recorded.ChatType))
		anchor.UserID = textutil.FirstNonEmpty(anchor.UserID, strings.TrimSpace(recorded.UserID))
	}
	if anchor.ChatID == "" {
		return Anchor{}, false
	}
	ordinal := s.Deps.Tracker.NextContinuationOrdinal(threadID)
	messageID, err := s.Deps.Presenter.SendContinuationAnchor(s.Deps.Context(), anchor.ChatID, goal, ordinal)
	if err != nil {
		return Anchor{}, false
	}
	anchor.MessageID = strings.TrimSpace(messageID)
	if anchor.MessageID == "" {
		return Anchor{}, false
	}
	s.Deps.Tracker.RecordAnchor(anchor)
	return anchor, true
}

func (s Service) findGoalContinuationSession(threadID string) (string, *conversation.Session) {
	if anchor, ok := s.Deps.Tracker.Anchor(threadID); ok {
		if sess := s.Deps.Repository.Session(anchor.SessionKey); sess != nil && strings.TrimSpace(sess.ActiveThreadID) == threadID {
			return anchor.SessionKey, sess
		}
	}
	for _, sess := range s.Deps.Repository.Sessions() {
		if sess == nil || !s.Deps.BelongsToFrontend(sess.Key) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == threadID {
			return sess.Key, sess
		}
	}
	return "", nil
}

func uniqueStrings(values []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}
