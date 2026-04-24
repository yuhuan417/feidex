package app

type appUpgradeService struct {
	app *App
}

func newAppUpgradeService(app *App) appUpgradeService {
	return appUpgradeService{app: app}
}
