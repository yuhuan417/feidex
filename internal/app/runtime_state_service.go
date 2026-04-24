package app

type runtimeStateService struct {
	app *App
}

func newRuntimeStateService(app *App) runtimeStateService {
	return runtimeStateService{app: app}
}
