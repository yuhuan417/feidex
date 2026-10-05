package feishuapp

import (
	"strings"

	frontendruntime "feidex/internal/runtime"
)

func SessionTaskRunner(actors *frontendruntime.SessionActors, runAsync func(func()) bool) func(string, func()) bool {
	return func(key string, fn func()) bool {
		if runAsync == nil {
			return false
		}
		return runAsync(func() { runSessionOnActor(actors, key, fn) })
	}
}

// runSessionAsync admits asynchronous work under the same frontend session
// actor used by the input dispatcher. Backend callbacks may arrive from a
// different goroutine, but their state transition still has one owner.
func runSessionAsync(lifecycle *frontendruntime.FrontendRuntime, runner func(func()), actors *frontendruntime.SessionActors, sessionKey string, fn func()) bool {
	if fn == nil {
		return false
	}
	if lifecycle == nil {
		return false
	}
	return lifecycle.Run(func() { runSessionOnActor(actors, sessionKey, fn) }, runner)
}

func runSessionOnActor(actors *frontendruntime.SessionActors, sessionKey string, fn func()) {
	if fn == nil {
		return
	}
	if actors == nil {
		fn()
		return
	}
	actors.Run("session:"+strings.TrimSpace(sessionKey), fn)
}
