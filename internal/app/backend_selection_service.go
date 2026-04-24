package app

type backendSelectionService struct {
	app *App
}

func newBackendSelectionService(app *App) backendSelectionService {
	return backendSelectionService{app: app}
}
