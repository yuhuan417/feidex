package app

type workspaceCommandService struct {
	app *App
}

func newWorkspaceCommandService(app *App) workspaceCommandService {
	return workspaceCommandService{app: app}
}

type threadCommandService struct {
	app *App
}

func newThreadCommandService(app *App) threadCommandService {
	return threadCommandService{app: app}
}
