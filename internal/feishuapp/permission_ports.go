package feishuapp

import (
	"context"
	"feidex/internal/application/threadsettings"
	domainbackend "feidex/internal/domain/backend"
)

type permissionPorts struct{ app *App }

func PermissionRuntime(a *App) threadsettings.PermissionRuntime { return permissionPorts{app: a} }
func PermissionTasks(a *App) threadsettings.PermissionTasks     { return permissionPorts{app: a} }
func PermissionFailure(a *App) threadsettings.PermissionFailure { return permissionPorts{app: a} }

func (p permissionPorts) ApplyPermission(ctx context.Context, key, mode string) error {
	core := p.app.runtimeView().currentClaudeCore()
	if core == nil || p.app.configView().configuredBackend() != domainbackend.BackendClaude {
		return nil
	}
	return core.SetPermissionMode(ctx, key, mode)
}
func (p permissionPorts) Run(key string, fn func()) bool {
	return runAsync(p.app, func() { runSession(p.app, key, fn) })
}
func (p permissionPorts) PermissionFailed(messageID, key string, err error) {
	patchClaudePermissionMenuRuntimeFailure(p.app, messageID, key, err)
}
