package app

// lifecycleCoordinator owns submission queueing, turn startup, and turn completion.
type lifecycleCoordinator struct {
	app *App
}

func newLifecycleCoordinator(app *App) *lifecycleCoordinator {
	return &lifecycleCoordinator{app: app}
}
