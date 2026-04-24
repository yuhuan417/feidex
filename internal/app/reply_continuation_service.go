package app

type replyContinuationService struct {
	app *App
}

func newReplyContinuationService(app *App) replyContinuationService {
	return replyContinuationService{app: app}
}
