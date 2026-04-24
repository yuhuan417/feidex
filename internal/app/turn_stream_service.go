package app

type turnStreamService struct {
	app *App
}

func newTurnStreamService(app *App) turnStreamService {
	return turnStreamService{app: app}
}
