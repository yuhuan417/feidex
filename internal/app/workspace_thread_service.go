package app

type workspaceThreadService struct {
	app *App
}

func newWorkspaceThreadService(app *App) workspaceThreadService {
	return workspaceThreadService{app: app}
}
