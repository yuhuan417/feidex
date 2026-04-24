package app

type debugService struct {
	app *App
}

func newDebugService(app *App) debugService {
	return debugService{app: app}
}
