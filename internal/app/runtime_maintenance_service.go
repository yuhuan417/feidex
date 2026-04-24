package app

type runtimeMaintenanceService struct {
	app *App
}

func newRuntimeMaintenanceService(app *App) runtimeMaintenanceService {
	return runtimeMaintenanceService{app: app}
}
