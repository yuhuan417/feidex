package feishuapp

import (
	"context"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	"feidex/internal/application/conversation"
	"feidex/internal/application/upgrade"
	"feidex/internal/config"
	domainconversation "feidex/internal/domain/conversation"
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

type MaintenanceCommandInputs struct {
	Context           func() context.Context
	Repository        maintenance.StateProvider
	Poller            upgrade.Poller
	Feishu            FeishuClient
	FrontendID        string
	EffectRunner      backendruntime.EffectRunner
	Config            *config.Config
	QueueNotification func(state.FrontendCardNotification)
	ReadyChatIDs      func([]*domainconversation.Session) []string
	RunAsync          func(func())
}

func BuildMaintenanceCommands(inputs MaintenanceCommandInputs) appmaintenance.RuntimeMaintenanceService {
	return appmaintenance.NewRuntimeMaintenanceService(appmaintenance.Dependencies{
		Context: inputs.Context, Repository: inputs.Repository, Poller: inputs.Poller,
		ArtifactClient:   inputs.Feishu,
		Outbound:         newEffectOutbound(inputs.FrontendID, inputs.EffectRunner),
		Renderer:         simpleStatusCardRenderer{client: inputs.Feishu},
		PermissionNotify: maintenancePermissionNotifier{client: inputs.Feishu},
		MenuBody:         menuCardBody,
		Workspaces: func() []config.Workspace {
			if inputs.Config == nil {
				return nil
			}
			return inputs.Config.Workspaces
		},
		QueueNotification: inputs.QueueNotification,
		ReadyChatIDs:      inputs.ReadyChatIDs,
		RunAsync:          inputs.RunAsync,
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
