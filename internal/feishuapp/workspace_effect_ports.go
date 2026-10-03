package feishuapp

import (
	"context"
	"feidex/internal/application/workspace"
	"feidex/internal/runtime"
)

type workspaceEffectRuntime struct{ app *App }

func (r workspaceEffectRuntime) ClearLive(key string) { clearSessionLiveThread(r.app, key) }
func (r workspaceEffectRuntime) Run(key string, job func()) bool {
	return r.app.runtimeOwner.Lifecycle.Run(func() { runSession(r.app, key, job) }, r.app.asyncRunner)
}
func (r workspaceEffectRuntime) Replay(ctx context.Context, id string) error {
	return r.app.bindings.BindingReplay.Replay(ctx, id)
}
func WorkspaceEffectRuntime(a *App) workspace.EffectRuntime { return workspaceEffectRuntime{app: a} }

func BindingReplayPorts(a *App) (*runtime.SessionActors, runtime.EffectRunner) {
	return a.sessionActorRuntime(), newEffectRunner(a)
}
