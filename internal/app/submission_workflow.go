package app

// submissionWorkflow owns the submission queue and turn lifecycle flow.
type submissionWorkflow struct {
	app *App
}

func newSubmissionWorkflow(app *App) *submissionWorkflow {
	return &submissionWorkflow{app: app}
}
