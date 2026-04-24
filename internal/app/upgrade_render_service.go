package app

type upgradeRenderService struct {
	app *App
}

func newUpgradeRenderService(app *App) upgradeRenderService {
	return upgradeRenderService{app: app}
}
