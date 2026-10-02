package app

import (
	"context"
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/state"
)

func newSubmissionCleanup(a *App) maintenance.SubmissionCleanup {
	return maintenance.SubmissionCleanup{Repository: a.State(), Runtime: newRuntimeStateService(a)}
}
func newStartupRecovery(a *App) maintenance.StartupRecovery {
	if a == nil {
		return maintenance.StartupRecovery{}
	}
	return maintenance.NewStartupRecovery(maintenance.RecoveryDependencies{
		Context: a.Context, Repository: a.State(), RecoveryMu: &a.frontendRecoveryMu,
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(a) },
		Workspace:          func(id string) *config.Workspace { return config.FindWorkspace(a.cfg, id) },
		ResetLiveThreads:   func() { a.liveThreads = frontendruntime.NewLiveThreads() },
		ClearLiveThread:    func(key string) { clearSessionLiveThread(a, key) },
		BelongsToFrontend:  func(key string) bool { return sessionBelongsToFrontend(a, key) },
		BackendConfigured:  func() bool { return hasConfiguredBackend(a) },
		BeginRecovery: func() func() {
			if runtime := backendRuntime(a); runtime != nil {
				return runtime.beginStartupRecoveryScope(a)
			}
			return func() {}
		},
		EffectiveModel: func(sess *conversation.Session) string {
			return modelConfigSnapshot(a, sess, configuredBackend(a)).Model
		},
		RecoverConversation: func(key, workspaceID string, sess *conversation.Session, ws *config.Workspace, model string) {
			recoverStartupConversation(a, key, workspaceID, sess, ws, model)
		},
		ExpireRequests:     func() { newRuntimeMaintenanceService(a).ExpirePendingRequestsOnStartup() },
		CleanupAttachments: func() { newRuntimeMaintenanceService(a).CleanupExpiredAttachments() },
		SendText: func(ctx context.Context, id, text string) error {
			if a.feishu == nil {
				return nil
			}
			return sendTextEffect(ctx, a, id, text)
		},
	})
}
func newRuntimeMaintenanceService(a *App) appmaintenance.RuntimeMaintenanceService {
	return appmaintenance.NewRuntimeMaintenanceService(appmaintenance.Dependencies{
		Context: a.Context, Store: a.store, Repository: a.State(), Client: a.feishu, MenuBody: menuCardBody,
		Workspaces: func() []config.Workspace {
			if a.cfg == nil {
				return nil
			}
			return a.cfg.Workspaces
		},
		QueueNotification: func(note state.FrontendCardNotification) { queueFrontendCardNotification(a, note) },
		ReadyChatIDs:      newStartupRecovery(a).FrontendStartupReadyChatIDs,
		RunAsync:          func(fn func()) { runAsync(a, fn) },
	})
}
