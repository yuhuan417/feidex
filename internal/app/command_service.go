package app

type commandService struct {
	app *App
}

func newCommandService(app *App) commandService {
	return commandService{app: app}
}
