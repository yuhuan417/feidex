package app

type backendConfigurationService struct {
	app *App
}

func newBackendConfigurationService(app *App) backendConfigurationService {
	return backendConfigurationService{app: app}
}
