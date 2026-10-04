package feishuapp

import (
	"context"
	"feidex/internal/application/workspace"
	"feidex/internal/runtime"
	"strings"
)

type workspaceEffectRuntime struct {
	lifecycle   *runtime.FrontendRuntime
	executor    func(func())
	actors      *runtime.SessionActors
	liveThreads *runtime.LiveThreads
	replay      runtime.BindingReplay
}

func (r workspaceEffectRuntime) ClearLive(key string) { r.liveThreads.Clear(key) }
func (r workspaceEffectRuntime) Run(key string, job func()) bool {
	return r.lifecycle.Run(func() {
		if job != nil {
			r.actors.Run("session:"+strings.TrimSpace(key), job)
		}
	}, r.executor)
}
func (r workspaceEffectRuntime) Replay(ctx context.Context, id string) error {
	return r.replay.Replay(ctx, id)
}
func WorkspaceEffectRuntime(lifecycle *runtime.FrontendRuntime, executor func(func()), actors *runtime.SessionActors, liveThreads *runtime.LiveThreads, replay runtime.BindingReplay) workspace.EffectRuntime {
	return workspaceEffectRuntime{lifecycle: lifecycle, executor: executor, actors: actors, liveThreads: liveThreads, replay: replay}
}

// BindingReplayPorts resolves its two inputs eagerly; it needs the session
// actors and the runtime owner, not the frontend aggregate.
func BindingReplayPorts(actors *runtime.SessionActors, runtimeOwner *runtime.FrontendOwner) (*runtime.SessionActors, runtime.EffectRunner) {
	return actors, newEffectRunner(runtimeOwner)
}
