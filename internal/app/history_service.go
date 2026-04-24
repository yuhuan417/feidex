package app

type historyService struct {
	app *App
}

func newHistoryService(app *App) historyService {
	return historyService{app: app}
}
