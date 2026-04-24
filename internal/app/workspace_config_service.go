package app

type workspaceConfigService struct {
	app *App
}

func newWorkspaceConfigService(app *App) workspaceConfigService {
	return workspaceConfigService{app: app}
}
