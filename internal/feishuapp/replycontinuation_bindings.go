package feishuapp

import (
	"context"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/continuation"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"sync"
)

func ContinuationPorts(
	cfg *config.Config, mu *sync.RWMutex, contextFn func() context.Context,
	store *appstate.Store, owner *frontendruntime.FrontendOwner,
	queue *appsubmission.SubmissionQueueService,
	frontendID string, frontendConfigIndex int, client FeishuClient,
) continuation.Dependencies {
	view := frontendConfigView{cfg: cfg, mu: mu, frontendID: frontendID, frontendConfigIndex: frontendConfigIndex}
	runtime := runtimeView{owner: owner}
	deps := continuation.Dependencies{
		Backend: func() string {
			current := view
			current.backend = owner.Backend()
			return current.configuredBackend()
		},
		FrontendID: frontendID, DefaultWorkspaceID: view.defaultWorkspaceID,
		Workspace:      func(id string) *config.Workspace { return config.FindWorkspace(cfg, id) },
		MakeSessionKey: view.makeSessionKey,
	}

	deps.GetSession = store.Session
	deps.SaveSession = store.SaveSession
	deps.GetMessageLink = store.MessageLink
	deps.SaveMessageLink = store.SaveMessageLink
	deps.CreateSubmission = store.CreateSubmission
	deps.HasInFlightSubmission = conversation.HasInFlightSubmission
	deps.Context = contextFn
	deps.Steer = func(ctx context.Context, threadID, turnID string, sub *domainsubmission.Submission) error {
		gateway, err := runtime.requireCodexGateway()
		if err != nil {
			return err
		}
		return gateway.SteerTurn(ctx, threadID, turnID, sub)
	}
	deps.StartSubmission = queue.StartNextClaudeSubmissionWithFailureNotice
	deps.StartSteerSubmission = func(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *config.Workspace, notifyFailure bool) error {
		return queue.StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, true)
	}
	deps.ResolveInboundAttachments = func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
		return resolveInboundAttachments(cfg, contextFn, client, msg, workspaceID, sessionKey)
	}

	return deps
}
