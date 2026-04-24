package app

type workspaceActionService struct {
	app *App
}

func newWorkspaceActionService(app *App) workspaceActionService {
	return workspaceActionService{app: app}
}

type threadActionService struct {
	app *App
}

func newThreadActionService(app *App) threadActionService {
	return threadActionService{app: app}
}
