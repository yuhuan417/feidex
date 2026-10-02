package app

import (
	"feidex/internal/app/appcore"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
)

func newWorkspaceThreadService(a *App) *appworkspacecmd.ThreadService {
	return serviceFor(a, "workspaceThreadService", func() *appworkspacecmd.ThreadService {
		st := a.State()
		return appworkspacecmd.NewThreadService(appworkspacecmd.ThreadServiceDeps{
			App: a,
			State: appworkspacecmd.StateDeps{
				GetSession:  func(key string) *conversation.Session { return st.Session(key) },
				SaveSession: func(sess *conversation.Session) error { return st.SaveSession(sess) },
			},
			Threads: appworkspacecmd.ThreadDeps{
				MarkSessionThreadLive: func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },
			},
			SessionContext: appworkspacecmd.SessionContextDeps{
				SessionHasInFlight:     conversation.HasInFlightSubmission,
				SwitchSessionWorkspace: conversation.SwitchSessionWorkspace,
				ClearSessionThreadCtx:  conversation.ClearThreadContext,
				SetSessionThreadCtx:    conversation.SetThreadContext,
				SessionResetActiveOps:  conversation.ResetActiveOperations,
			},
			Codex: appworkspacecmd.CodexDeps{
				RequireCodexClient: func() (appworkspacecmd.CodexClient, error) { return requireCodexClient(a) },
				BuildThreadStartParams: func(ws *config.Workspace, sess *conversation.Session, effectiveModel string) codexrpc.ThreadStartParams {
					return buildThreadStartParams(a, ws, sess, effectiveModel)
				},
				BuildThreadConfig: func(sess *conversation.Session) map[string]any { return codexAuxiliaryConfig(a, sess) },
			},
			Claude: appworkspacecmd.ClaudeDeps{
				RequireClaudeCore: func() (appcore.ClaudeCore, error) { return a.Claude(), nil },
			},
		})
	})
}
