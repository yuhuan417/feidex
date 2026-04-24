package app

type finalCardPatchService struct {
	app *App
}

func newFinalCardPatchService(app *App) finalCardPatchService {
	return finalCardPatchService{app: app}
}
