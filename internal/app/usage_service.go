package app

type usageService struct {
	app *App
}

func newUsageService(app *App) usageService {
	return usageService{app: app}
}
