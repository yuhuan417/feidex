package feishuapp

import appthreadmenu "feidex/internal/adapter/feishu/threadmenu"

import domainbackend "feidex/internal/domain/backend"

// These test-only conveniences keep fixture setup readable without retaining
// compatibility facades in the production App API.
func (a *App) Backend() string {
	if a == nil || a.runtimeOwner == nil {
		return ""
	}
	return a.runtimeOwner.Backend()
}

func (a *App) SetBackend(backend string) {
	if a == nil || a.runtimeOwner == nil {
		return
	}
	a.runtimeOwner.SetBackend(domainbackend.NormalizeBackend(backend))
	if a.stateView != nil {
		a.stateView.SetBackend(a.runtimeOwner.Backend())
	}
}

func (a *App) AsyncRunner() func(func()) {
	if a == nil {
		return nil
	}
	return a.asyncRunner
}

func (a *App) ConfigPath() string {
	if a == nil {
		return ""
	}
	return a.cfgPath
}

func (a *App) FrontendConfigIndex() int {
	if a == nil {
		return -1
	}
	return a.frontendConfigIndex
}

func ThreadMenuDependencies(a *App) appthreadmenu.Dependencies {
	if a == nil {
		return appthreadmenu.Dependencies{}
	}
	return BuildThreadMenuDependencies(ThreadMenuInputs{
		Runtime: a.BackendRuntimeDeps(), Store: a.store, State: a.State(),
		Conversations: a.bindings.Conversations, BindingScope: BindingScope{scope: a.bindings.BindingCommands.scope},
		ConversationQuery: a.bindings.ConversationQuery, PendingQueue: a.bindings.PendingQueue,
		WorkspaceConfiguration: a.bindings.WorkspaceConfiguration, WorkspaceSelection: a.bindings.WorkspaceSelection,
		ConversationControls: a.bindings.ConversationControls, ThreadSettings: a.bindings.ThreadSettings,
		PermissionSettings: a.bindings.PermissionSettings, BackendActions: a.bindings.BackendActions,
		AutoRetry: a.bindings.AutoRetry, CompleteMenuCommand: a.CompleteMenuCommand,
	})
}
