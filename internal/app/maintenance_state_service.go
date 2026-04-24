package app

type maintenanceStateService struct {
	app *App
}

func newMaintenanceStateService(app *App) maintenanceStateService {
	return maintenanceStateService{app: app}
}
