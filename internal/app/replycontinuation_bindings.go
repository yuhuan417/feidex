package app

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/application/continuation"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func newReplyContinuationService(a *App) *continuation.Service {
	svc := &continuation.Service{
		Backend:    func() string { return configuredBackend(a) },
		FrontendID: a.FrontendID(), DefaultWorkspaceID: func() string { return defaultWorkspaceID(a) },
		Workspace:      func(id string) *config.Workspace { return config.FindWorkspace(a.cfg, id) },
		MakeSessionKey: func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) },
	}

	// Wire callback function fields that need *App internals.
	svc.GetSession = func(key string) *conversation.Session {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.Session(key)
	}
	svc.SaveSession = func(sess *conversation.Session) error {
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
	svc.CreateSubmission = func(sub *domainsubmission.Submission) (string, error) {
		st := a.State()
		if st == nil {
			return "", nil
		}
		return st.CreateSubmission(sub)
	}
	svc.HasInFlightSubmission = func(sess *conversation.Session) bool {
		return conversation.HasInFlightSubmission(sess)
	}
	svc.TrySteer = func(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *conversation.Session) (bool, error) {
		if configuredBackend(a) == backendClaude {
			return svc.TryClaudeReplyContinuation(msg, link, sessionKey, sess)
		}
		return continuation.TryCodexReplyContinuation(continuation.CodexReplyContinuationDeps{
			Context: a.Context, Steer: func(ctx context.Context, threadID, turnID string, sub *domainsubmission.Submission) error {
				client, err := requireCodexClient(a)
				if err != nil {
					return err
				}
				return (codexadapter.Gateway{Client: client}).SteerTurn(ctx, threadID, turnID, sub)
			},
			ResolveInboundAttachments: svc.ResolveInboundAttachments,
			PendingInputSessionKey:    svc.PendingInputSessionKey, CollectPendingStagedImages: svc.CollectPendingStagedImages,
			ClearPendingStagedImages: svc.ClearPendingStagedImages, SaveSession: svc.SaveSession, DefaultWorkspaceID: svc.DefaultWorkspaceID,
		}, msg, link, sessionKey, sess)
	}
	svc.StartSubmission = func(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *config.Workspace, notifyFailure bool) error {
		return newSubmissionQueueServiceFromApp(a).StartNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
	}
	svc.StartSteerSubmission = func(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *config.Workspace, notifyFailure bool) error {
		return newSubmissionQueueServiceFromApp(a).StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, true)
	}
	svc.ResolveInboundAttachments = func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
		return resolveInboundAttachments(a, msg, workspaceID, sessionKey)
	}

	return svc
}
