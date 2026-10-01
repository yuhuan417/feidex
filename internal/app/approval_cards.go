package app

type outboundCardService struct {
	app *App
}

func newOutboundCardService(app *App) outboundCardService {
	return serviceFor(app, "outboundCardService", func() outboundCardService {
		return outboundCardService{app: app}
	})
}
