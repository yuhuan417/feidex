package app

type workspaceRenderService struct {
	app *App
}

func newWorkspaceRenderService(app *App) workspaceRenderService {
	return workspaceRenderService{app: app}
}
