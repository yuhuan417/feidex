package feishuapp

import "strings"

func SessionTaskRunner(a *App) func(string, func()) bool {
	return func(key string, fn func()) bool {
		return runAsync(a, func() { runSession(a, key, fn) })
	}
}

// runSessionAsync admits asynchronous work under the same frontend session
// actor used by the input dispatcher. Backend callbacks may arrive from a
// different goroutine, but their state transition still has one owner.
func runSessionAsync(a *App, sessionKey string, fn func()) {
	if fn == nil {
		return
	}
	runAsync(a, func() {
		runSession(a, sessionKey, fn)
	})
}

// runSession executes a state transition under the owning session actor. It
// is synchronous so protocol callback streams retain their source ordering;
// callers that must detach from the callback goroutine should use
// runSessionAsync.
func runSession(a *App, sessionKey string, fn func()) {
	if fn == nil {
		return
	}
	if a == nil {
		fn()
		return
	}
	a.sessionActorRuntime().Run("session:"+strings.TrimSpace(sessionKey), fn)
}
