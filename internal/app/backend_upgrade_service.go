package app

type backendUpgradeService struct {
	app *App
}

func newBackendUpgradeService(app *App) backendUpgradeService {
	return backendUpgradeService{app: app}
}
