package app

type workspaceService struct {
	app *App
}

func newWorkspaceService(app *App) workspaceService {
	return workspaceService{app: app}
}

type threadService struct {
	app *App
}

func newThreadService(app *App) threadService {
	return threadService{app: app}
}
