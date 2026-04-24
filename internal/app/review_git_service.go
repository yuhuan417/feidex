package app

type reviewGitService struct {
	app *App
}

func newReviewGitService(app *App) reviewGitService {
	return reviewGitService{app: app}
}
