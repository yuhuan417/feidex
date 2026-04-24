package app

type pendingQueueService struct {
	app *App
}

func newPendingQueueService(app *App) pendingQueueService {
	return pendingQueueService{app: app}
}
