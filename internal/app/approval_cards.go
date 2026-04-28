package app

type outboundCardService struct {
	app *App
}

func newOutboundCardService(app *App) outboundCardService {
	return outboundCardService{app: app}
}
