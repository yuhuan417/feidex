package feishuapp

import (
	"context"
	"feidex/internal/application/threadsettings"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/identity"
	frontendruntime "feidex/internal/runtime"
)

type permissionRuntimePort struct {
	backend func() string
	core    func() ClaudeCore
}

type permissionTasksPort struct {
	lifecycle *frontendruntime.FrontendRuntime
	actors    *frontendruntime.SessionActors
	runAsync  func(func())
}

type permissionFailurePort struct {
	render     func(string) (map[string]any, error)
	frontendID identity.FrontendID
	runner     frontendruntime.EffectRunner
}

func PermissionRuntime(backend func() string, core func() ClaudeCore) threadsettings.PermissionRuntime {
	return permissionRuntimePort{backend: backend, core: core}
}

func PermissionTasks(lifecycle *frontendruntime.FrontendRuntime, actors *frontendruntime.SessionActors, runAsync func(func())) threadsettings.PermissionTasks {
	return permissionTasksPort{lifecycle: lifecycle, actors: actors, runAsync: runAsync}
}

func PermissionFailure(render func(string) (map[string]any, error), frontendID string, runner frontendruntime.EffectRunner) threadsettings.PermissionFailure {
	return permissionFailurePort{render: render, frontendID: identity.FrontendID(frontendID), runner: runner}
}

func (p permissionRuntimePort) ApplyPermission(ctx context.Context, key, mode string) error {
	if p.core == nil {
		return nil
	}
	core := p.core()
	if core == nil || p.backend == nil || p.backend() != domainbackend.BackendClaude {
		return nil
	}
	return core.SetPermissionMode(ctx, key, mode)
}

func (p permissionTasksPort) Run(key string, fn func()) bool {
	if fn == nil || p.lifecycle == nil {
		return false
	}
	return p.lifecycle.Run(func() { runSessionOnActor(p.actors, key, fn) }, p.runAsync)
}

func (p permissionFailurePort) PermissionFailed(messageID, key string, err error) {
	patchClaudePermissionMenuRuntimeFailure(p.render, p.frontendID, p.runner, messageID, key, err)
}
