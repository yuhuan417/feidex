package app

type pendingInputService struct {
	app *App
}

func newPendingInputService(app *App) pendingInputService {
	return pendingInputService{app: app}
}
