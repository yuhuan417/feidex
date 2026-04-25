package app

// BackendDeps is a narrow interface for backend facade implementations.
// It decouples facades from the full *App concrete type; free functions
// and service constructors still accept *App and can be migrated to this
// interface incrementally.
type BackendDeps interface {
	App() *App
}

func (a *App) App() *App { return a }
