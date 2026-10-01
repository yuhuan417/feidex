package app

type pendingInputService struct {
	app *App
}

func newPendingInputService(app *App) pendingInputService {
	return serviceFor(app, "pendingInputService", func() pendingInputService {
		return pendingInputService{app: app}
	})
}
