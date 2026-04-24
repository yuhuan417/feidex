package app

type workspaceManagementService struct {
	app *App
}

func newWorkspaceManagementService(app *App) workspaceManagementService {
	return workspaceManagementService{app: app}
}
