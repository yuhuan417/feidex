package app

type skillsService struct {
	app *App
}

func newSkillsService(app *App) skillsService {
	return skillsService{app: app}
}
