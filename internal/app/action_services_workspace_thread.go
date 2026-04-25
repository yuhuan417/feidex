package app

type workspaceService struct {
	app *App
}

func newWorkspaceService(app *App) workspaceService {
	return workspaceService{app: app}
}
