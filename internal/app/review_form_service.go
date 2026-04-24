package app

type reviewFormService struct {
	app *App
}

func newReviewFormService(app *App) reviewFormService {
	return reviewFormService{app: app}
}
