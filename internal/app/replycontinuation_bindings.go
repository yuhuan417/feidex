package app

import (
	"sync"

	"feidex/internal/app/appcore"
	"feidex/internal/app/replycontinuation"
	"feidex/internal/app/sessionctx"
	"feidex/internal/app/submission"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// replyContinuationAppAdapter satisfies replycontinuation.App by delegating
// to *App accessor methods.
type replyContinuationAppAdapter struct{ app *App }

func (a replyContinuationAppAdapter) Config() *config.Config   { return a.app.Config() }
func (a replyContinuationAppAdapter) ConfigMu() *sync.RWMutex  { return a.app.ConfigMu() }
func (a replyContinuationAppAdapter) Backend() string          { return a.app.Backend() }
func (a replyContinuationAppAdapter) FrontendID() string       { return a.app.FrontendID() }
func (a replyContinuationAppAdapter) FrontendConfigIndex() int { return a.app.FrontendConfigIndex() }
func (a replyContinuationAppAdapter) Store() *state.Store      { return a.app.Store() }
func (a replyContinuationAppAdapter) DefaultWorkspaceID() string {
	return appcore.DefaultWorkspaceID(a.app)
}

// replyContinuationService wraps replycontinuation.Service to preserve the
// lowercase method names used throughout app/.
type replyContinuationService struct {
	inner *replycontinuation.Service
}

func newReplyContinuationService(a *App) replyContinuationService {
	svc := replycontinuation.NewService(replyContinuationAppAdapter{app: a})

	// Wire callback function fields that need *App internals.
	svc.GetSession = func(key string) *state.Session {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.Session(key)
	}
	svc.SaveSession = func(sess *state.Session) error {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.SaveSession(sess)
	}
	svc.GetMessageLink = func(messageID string) *state.MessageLink {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.MessageLink(messageID)
	}
	svc.SaveMessageLink = func(link *state.MessageLink) error {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.SaveMessageLink(link)
	}
	svc.CreateSubmission = func(sub *state.Submission) (string, error) {
		st := a.State()
		if st == nil {
			return "", nil
		}
		return st.CreateSubmission(sub)
	}
	svc.HasInFlightSubmission = func(sess *state.Session) bool {
		return sessionctx.HasInFlightSubmission(sess)
	}
	svc.TrySteer = func(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
		return conversationBackend(a).TryReplyContinuation(msg, link, sessionKey, sess)
	}
	svc.StartSubmission = func(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
		return newClaudeSubmissionService(a).startNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
	}
	svc.StartSteerSubmission = func(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
		return newClaudeSubmissionService(a).startNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, true)
	}
	svc.ResolveInboundAttachments = func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
		return resolveInboundAttachments(a, msg, workspaceID, sessionKey)
	}

	return replyContinuationService{inner: svc}
}

func (s replyContinuationService) replyRootTurnLink(msg *feishu.InboundMessage) *state.MessageLink {
	return s.inner.ReplyRootTurnLink(msg)
}

func (s replyContinuationService) messageLinkMatchesCurrentBackend(link *state.MessageLink) bool {
	return s.inner.MessageLinkMatchesCurrentBackend(link)
}

func (s replyContinuationService) sessionKeyForInboundMessage(msg *feishu.InboundMessage, link *state.MessageLink) string {
	return s.inner.SessionKeyForInboundMessage(msg, link)
}

func (s replyContinuationService) pendingInputSessionKey(msg *feishu.InboundMessage) string {
	return s.inner.PendingInputSessionKey(msg)
}

func (s replyContinuationService) collectPendingStagedImages(targetSessionKey, bucketSessionKey string) []state.SessionStagedImage {
	return s.inner.CollectPendingStagedImages(targetSessionKey, bucketSessionKey)
}

func (s replyContinuationService) clearPendingStagedImages(targetSessionKey, bucketSessionKey string) error {
	return s.inner.ClearPendingStagedImages(targetSessionKey, bucketSessionKey)
}

func (s replyContinuationService) trySteerInboundReply(msg *feishu.InboundMessage, link *state.MessageLink) (bool, error) {
	return s.inner.TrySteerInboundReply(msg, link)
}

func (s replyContinuationService) tryClaudeReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return s.inner.TryClaudeReplyContinuation(msg, link, sessionKey, sess)
}

func (s replyContinuationService) continueClaudeSessionWithText(sessionKey, text string) error {
	return s.inner.ContinueClaudeSessionWithText(sessionKey, text)
}

func (s replyContinuationService) buildClaudeContinuationSubmissionFromMessage(msg *feishu.InboundMessage, sessionKey string, sess *state.Session, bindOnlyCurrentRoot bool) (*state.Submission, error) {
	return s.inner.BuildClaudeContinuationSubmissionFromMessage(msg, sessionKey, sess, bindOnlyCurrentRoot)
}

func (s replyContinuationService) startClaudeContinuationSubmission(sessionKey string, sub *state.Submission, notifyFailure bool) error {
	return s.inner.StartClaudeContinuationSubmission(sessionKey, sub, notifyFailure)
}

func (s replyContinuationService) recordSubmissionSourceLinks(sub *state.Submission) {
	s.inner.RecordSubmissionSourceLinks(sub)
}

func (s replyContinuationService) recordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string) {
	s.inner.RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID)
}

func (s replyContinuationService) recordTurnMessageLink(messageID, sessionKey, threadID, turnID string) {
	s.inner.RecordTurnMessageLink(messageID, sessionKey, threadID, turnID)
}

// Exported wrappers for sub-package interface satisfaction.
func (s replyContinuationService) RecordSubmissionSourceLinks(sub *state.Submission) {
	s.recordSubmissionSourceLinks(sub)
}
func (s replyContinuationService) RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string) {
	s.recordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID)
}

// Re-export staged image helpers for use by other app/ code.
var (
	stagedImageAttachments        = submission.StagedImageAttachments
	stagedImageSourceMessageIDs   = submission.StagedImageSourceMessageIDs
	stagedImageRootMessageIDs     = submission.StagedImageRootMessageIDs
	sourceMessageIDsForSubmission = submission.SourceMessageIDs
)
