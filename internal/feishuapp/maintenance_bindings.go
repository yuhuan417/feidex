package feishuapp

import (
	"context"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	"feidex/internal/config"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/state"
)

// StartupRecoveryPorts takes the maintenance-command entry point it needs
// from a service constructed after it.
func StartupRecoveryPorts(a *App, cleanupExpiredAttachments func()) maintenance.RecoveryDependencies {
	return maintenance.RecoveryDependencies{
		Context: a.Context, Repository: a.State(), RecoveryMu: &a.runtimeView().ensureRuntimeOwner().RecoveryMu,
		ResetLiveThreads:  func() { resetAppLiveThreadTracker(a) },
		BelongsToFrontend: func(key string) bool { return a.configView().sessionBelongsToFrontend(key) },
		BackendConfigured: func() bool { return a.configView().hasConfiguredBackend() },
		BeginRecovery: func() func() {
			if runtime := backendRuntime(a); runtime != nil {
				return runtime.BeginStartupRecoveryScope(backendRuntimeContextForApp(a.BackendRuntimeDeps()))
			}
			return func() {}
		},
		RestoreState:       func() error { return a.bindings.ConversationRecovery.Restore() },
		ResetState:         a.bindings.StartupState.Reset,
		CleanupAttachments: cleanupExpiredAttachments,
		SendText: func(ctx context.Context, id, text string) error {
			if a.feishu == nil {
				return nil
			}
			return sendTextEffect(ctx, a, id, text)
		},
	}
}
func BuildMaintenanceCommands(a *App) appmaintenance.RuntimeMaintenanceService {
	return appmaintenance.NewRuntimeMaintenanceService(appmaintenance.Dependencies{
		Context: a.Context, Repository: a.State(), Poller: a.bindings.UpgradePoller,
		ArtifactClient:   a.feishu,
		Outbound:         newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)),
		Renderer:         simpleStatusCardRenderer{client: a.feishu},
		PermissionNotify: maintenancePermissionNotifier{client: a.feishu},
		MenuBody:         menuCardBody,
		Workspaces: func() []config.Workspace {
			if a.cfg == nil {
				return nil
			}
			return a.cfg.Workspaces
		},
		QueueNotification: func(note state.FrontendCardNotification) { queueFrontendCardNotification(a, note) },
		ReadyChatIDs:      a.bindings.StartupRecovery.FrontendStartupReadyChatIDs,
		RunAsync:          func(fn func()) { runAsync(a, fn) },
	})
}

type maintenancePermissionNotifier struct{ client interface{} }

func (n maintenancePermissionNotifier) NotifyPermissionIssue(target appfeishuwrap.NotifyTarget, err error) {
	if notifier, ok := n.client.(interface {
		NotifyPermissionIssue(appfeishuwrap.NotifyTarget, error)
	}); ok {
		notifier.NotifyPermissionIssue(target, err)
	}
}
