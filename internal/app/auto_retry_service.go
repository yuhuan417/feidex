package app

type autoRetryService struct {
	app *App
}

func newAutoRetryService(app *App) autoRetryService {
	return autoRetryService{app: app}
}
