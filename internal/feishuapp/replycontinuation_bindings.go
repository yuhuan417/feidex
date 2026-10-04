package feishuapp

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

func ContinuationPorts(a *App) continuation.Dependencies {
	deps := continuation.Dependencies{
		Backend:    func() string { return a.configView().configuredBackend() },
		FrontendID: a.FrontendID(), DefaultWorkspaceID: func() string { return a.configView().defaultWorkspaceID() },
		Workspace:      func(id string) *config.Workspace { return config.FindWorkspace(a.cfg, id) },
		MakeSessionKey: func(msg *feishu.InboundMessage) string { return a.configView().makeSessionKey(msg) },
	}

	// Wire the consumer-owned ports that need *App internals.
	deps.GetSession = func(key string) *conversation.Session {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.Session(key)
	}
	deps.SaveSession = func(sess *conversation.Session) error {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.SaveSession(sess)
	}
	deps.GetMessageLink = func(messageID string) *state.MessageLink {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.MessageLink(messageID)
	}
	deps.SaveMessageLink = func(link *state.MessageLink) error {
		st := a.State()
		if st == nil {
			return nil
		}
		return st.SaveMessageLink(link)
	}
	deps.CreateSubmission = func(sub *domainsubmission.Submission) (string, error) {
		st := a.State()
		if st == nil {
			return "", nil
		}
		return st.CreateSubmission(sub)
	}
	deps.HasInFlightSubmission = func(sess *conversation.Session) bool {
		return conversation.HasInFlightSubmission(sess)
	}
	deps.Context = a.Context
	deps.Steer = func(ctx context.Context, threadID, turnID string, sub *domainsubmission.Submission) error {
		client, err := requireCodexClient(a)
		if err != nil {
			return err
		}
		return (codexadapter.Gateway{Client: client}).SteerTurn(ctx, threadID, turnID, sub)
	}
	queue := a.bindings.Submissions
	deps.StartSubmission = queue.StartNextClaudeSubmissionWithFailureNotice
	deps.StartSteerSubmission = func(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *config.Workspace, notifyFailure bool) error {
		return queue.StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, true)
	}
	deps.ResolveInboundAttachments = func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
		return resolveInboundAttachments(a, msg, workspaceID, sessionKey)
	}

	return deps
}
