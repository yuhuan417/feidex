package app

// submissionCoordinator owns submission queueing and dispatching to backends.
type submissionCoordinator struct {
	app *App
}

func newSubmissionCoordinator(app *App) *submissionCoordinator {
	return &submissionCoordinator{app: app}
}
