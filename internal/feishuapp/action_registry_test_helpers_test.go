package feishuapp

func newCardActionService(app *App) cardActionDispatcher {
	return cardActionDispatcher{inner: app.bindings.CardActions}
}

func newMenuActionService(app *App) cardActionService {
	return cardActionService{app: app}
}
