package app

import (
	apphistorycmd "feidex/internal/app/historycmd"
)

func newHistoryService(app *App) apphistorycmd.Service {
	return newHistoryServiceInner(app)
}

var ()
