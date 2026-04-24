package app

type modelConfigService struct {
	app *App
}

func newModelConfigService(app *App) modelConfigService {
	return modelConfigService{app: app}
}
