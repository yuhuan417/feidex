package feishuapp

import (
	"context"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	"feidex/internal/application/conversation"
	"feidex/internal/config"
	backendruntime "feidex/internal/runtime"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/state"
)

type StartupRecoveryPortInputs struct {
	Runtime                   BackendRuntimeDeps
	StartupState              conversation.StartupState
	CleanupExpiredAttachments func()
	RestoreConversationState  func() error
	SendText                  func(context.Context, string, string) error
}

func StartupRecoveryPorts(inputs StartupRecoveryPortInputs) maintenance.RecoveryDependencies {
	runtimeDeps := inputs.Runtime
	owner := runtimeDeps.runtime.owner
	if owner == nil {
		return maintenance.RecoveryDependencies{}
	}
	liveThreads := owner.LiveThreads
	stateStore := runtimeDeps.stateView
	return maintenance.RecoveryDependencies{
		Context: owner.Lifecycle.Context, Repository: stateStore, RecoveryMu: &owner.RecoveryMu,
		ResetLiveThreads:  liveThreads.Reset,
		BelongsToFrontend: runtimeDeps.view.sessionBelongsToFrontend,
		BackendConfigured: func() bool { return runtimeDeps.currentBackend().view.hasConfiguredBackend() },
		BeginRecovery: func() func() {
			current := runtimeDeps.currentBackend()
			if runtime := backendruntime.BackendForKind(current.view.configuredBackend()); runtime != nil {
				return runtime.BeginStartupRecoveryScope(backendRuntimeContextForApp(current))
			}
			return func() {}
		},
		RestoreState:       inputs.RestoreConversationState,
		ResetState:         inputs.StartupState.Reset,
		CleanupAttachments: inputs.CleanupExpiredAttachments,
		SendText:           inputs.SendText,
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
