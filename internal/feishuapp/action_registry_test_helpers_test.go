package feishuapp

func newCardActionService(app *App) cardActionDispatcher {
	return cardActionDispatcher{inner: app.bindings.CardActions}
}
