package feishuapp

import (
	"context"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/state"
)

func StartupRecoveryPorts(a *App) maintenance.RecoveryDependencies {
	return maintenance.RecoveryDependencies{
		Context: a.Context, Repository: a.State(), RecoveryMu: &ensureRuntimeOwner(a).RecoveryMu,
		ResetLiveThreads:  func() { resetAppLiveThreadTracker(a) },
		BelongsToFrontend: func(key string) bool { return sessionBelongsToFrontend(a, key) },
		BackendConfigured: func() bool { return hasConfiguredBackend(a) },
		BeginRecovery: func() func() {
			if runtime := backendRuntime(a); runtime != nil {
				return runtime.BeginStartupRecoveryScope(backendRuntimeContextForApp(a))
			}
			return func() {}
		},
		RestoreState:       func() error { return a.bindings.ConversationRecovery.Restore() },
		ResetState:         a.bindings.StartupState.Reset,
		CleanupAttachments: func() { a.bindings.MaintenanceCommands.CleanupExpiredAttachments() },
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
		Outbound:         maintenanceOutbound{app: a},
		Renderer:         maintenanceCardRenderer{app: a},
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

type maintenanceOutbound struct{ app *App }

func (o maintenanceOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return patchCardEffect(ctx, o.app, messageID, card)
}

type maintenanceCardRenderer struct{ app *App }

func (r maintenanceCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}
