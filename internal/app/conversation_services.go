package app

import (
	"context"
	claudeadapter "feidex/internal/adapter/backend/claude"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/adapter/feishu/threadview"
	"feidex/internal/application/backendops"
	"feidex/internal/application/conversation"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	domain "feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	codexruntime "feidex/internal/runtime/codex"
	"strings"
)

func newConversationService(a *App) *conversation.Service {
	s := &conversation.Service{Deps: conversation.Dependencies{Context: a.Context(), Backend: configuredBackend(a), Repository: compositionkit.ConversationRepository{Repository: a.State(), Runner: newEffectRunner(a), Frontend: identity.FrontendID(a.FrontendID()), Context: a.Context()}, Live: sqLiveThreadAdapter{app: a}}}
	s.Deps.ModelSettings = newModelSnapshotService(a)
	if s.Deps.Backend == domainbackend.BackendClaude {
		s.Deps.Gateway = claudeadapter.ConversationGateway{Client: currentClaudeCore(a), Continue: newReplyContinuationService(a).ContinueClaudeSessionWithText}
	} else {
		s.Deps.Gateway = codexadapter.ConversationGateway{
			Client: func() (codexadapter.ConversationClient, error) { return requireCodexClient(a) },
			StartParams: func(r conversation.Request) backendops.ThreadStartConfig {
				return buildThreadStartParams(a, r.Workspace, r.Session, r.Model)
			},
			ResumeConfig: func(sess *domain.Session) map[string]any { return codexAuxiliaryConfig(a, sess) },
			ForkParams: func(r conversation.Request) backendops.ThreadForkRequest {
				return backendops.ThreadForkRequest{
					ThreadID:       strings.TrimSpace(r.Session.ActiveThreadID),
					Cwd:            r.Workspace.Cwd,
					ApprovalPolicy: effectiveBindingApprovalPolicy(a, r.Session, r.Workspace),
					SandboxMode:    effectiveBindingSandboxMode(a, r.Session, r.Workspace),
					ServiceTier:    effectiveBindingServiceTier(a, r.Session),
					Model:          r.Model,
					MultiAgentMode: effectiveBindingMultiAgentMode(a, r.Session, r.Workspace),
				}
			},
		}
	}
	return s
}
func renderThreadsCard(a *App, key string, all bool) (map[string]any, error) {
	sess := a.State().Session(key)
	ws := &a.cfg.Workspaces[0]
	if sess != nil {
		if selected := config.FindWorkspace(a.cfg, sess.WorkspaceID); selected != nil {
			ws = selected
		}
	}
	items, err := newConversationService(a).ListWorkspaceThreads(key, ws, all)
	if err != nil {
		return nil, err
	}
	if configuredBackend(a) == domainbackend.BackendClaude {
		return threadview.RenderClaudeThreadsCard(key, sess, ws, configuredBackend(a), a.cfg.Claude, items, all)
	}
	return threadview.RenderCodexThreadsCard(key, sess, *ws, configuredBackend(a), items, all)
}
func forkReplyMessage(a *App, id string) string {
	if configuredBackend(a) == domainbackend.BackendClaude {
		if strings.TrimSpace(id) == "" {
			return "prepared to fork current session. new Claude branch session will be created and switched on next message."
		}
		return "forked current session and switched to new branch session."
	}
	return "forked current thread and switched to new branch thread."
}
func renderConversationUsage(a *App, sess *domain.Session) string {
	if configuredBackend(a) == domainbackend.BackendClaude {
		return newUsageService(a).RenderClaudeUsageBody(sess)
	}
	return newUsageService(a).RenderCodexUsageBody(sess)
}
func interruptConversation(a *App, ctx context.Context, key string, sess *domain.Session) error {
	err := newConversationService(a).InterruptActiveTurn(ctx, key, sess)
	if err != nil && sess != nil && configuredBackend(a) == domainbackend.BackendCodex {
		updated := reconcileCompletedCodexTurn(a, sess.Key, sess)
		if updated == nil || updated.ActiveTurnID != sess.ActiveTurnID {
			return nil
		}
	}
	return err
}
func startupRecoveryDependencies(a *App) codexruntime.StartupRecoveryDeps {
	return codexruntime.StartupRecoveryDeps{
		Context: a.Context, CurrentClient: func() codexruntime.CodexRPCClient { return currentCodexClient(a) },
		RuntimeRecovering: func() bool { return codexRuntimeRecovering(a) },
		BuildThreadStartParams: func(ws *config.Workspace, sess *domain.Session, model string) backendops.ThreadStartConfig {
			return buildThreadStartParams(a, ws, sess, model)
		},
		BuildThreadConfig: func(sess *domain.Session) map[string]any { return codexAuxiliaryConfig(a, sess) },
		SaveSession:       a.State().SaveSession, SetThreadContext: domain.SetThreadContext, ClearThreadContext: domain.ClearThreadContext,
		MarkThreadLive: func(key, id string) { markSessionThreadLive(a, key, id) }, ClearSessionLiveThread: func(key string) { clearSessionLiveThread(a, key) },
	}
}
func recoverStartupConversation(a *App, key, workspaceID string, sess *domain.Session, ws *config.Workspace, model string) {
	if configuredBackend(a) == domainbackend.BackendClaude {
		codexruntime.RecoverClaudeStartupConversation(codexruntime.ClaudeStartupRecoveryDeps{Context: a.Context, MarkThreadLive: func(key, id string) { markSessionThreadLive(a, key, id) }}, key, workspaceID, sess)
		return
	}
	codexruntime.RecoverStartupConversation(startupRecoveryDependencies(a), key, workspaceID, sess, ws, model)
}
