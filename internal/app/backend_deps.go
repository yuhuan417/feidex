package app

// BackendDeps is an alias for AppDeps, kept for backward compatibility.
// New code should use AppDeps directly.
type BackendDeps = AppDeps

// App returns self, satisfying both AppDeps and BackendDeps.
func (a *App) App() *App { return a }
