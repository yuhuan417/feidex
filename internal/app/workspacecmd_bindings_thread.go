package app

import (
	"feidex/internal/app/appcore"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

type workspaceThreadBinding = appworkspacecmd.ThreadBinding

func newWorkspaceThreadServiceInner(a *App) *appworkspacecmd.ThreadService {
	st := a.State()
	return appworkspacecmd.NewThreadService(appworkspacecmd.ThreadServiceDeps{
		App: a,
		State: appworkspacecmd.StateDeps{
			GetSession:  func(key string) *state.Session { return st.Session(key) },
			SaveSession: func(sess *state.Session) error { return st.SaveSession(sess) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			MarkSessionThreadLive: func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },
		},
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     sessionHasInFlightSubmission,
			SwitchSessionWorkspace: switchSessionWorkspace,
			ClearSessionThreadCtx:  clearSessionThreadContext,
			SetSessionThreadCtx:    setSessionThreadContext,
			SessionResetActiveOps:  sessionResetActiveOperations,
		},
		Codex: appworkspacecmd.CodexDeps{
			RequireCodexClient: func() (appworkspacecmd.CodexClient, error) { return requireCodexClient(a) },
			BuildThreadStartParams: func(ws *config.Workspace, sess *state.Session, effectiveModel string) codexrpc.ThreadStartParams {
				return buildThreadStartParams(a, ws, sess, effectiveModel)
			},
		},
		Claude: appworkspacecmd.ClaudeDeps{
			RequireClaudeCore: func() (appcore.ClaudeCore, error) { return a.Claude(), nil },
		},
	})
}
