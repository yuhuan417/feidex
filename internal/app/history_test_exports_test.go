package app

import (
	apphistory "feidex/internal/app/apphistory"
	apphistorycmd "feidex/internal/app/historycmd"
)

type historyService = apphistorycmd.Service

func newHistoryService(app *App) historyService {
	return newHistoryServiceInner(app)
}

const historyPageSize = apphistorycmd.HistoryPageSize
const historyCommandUsage = apphistorycmd.HistoryCommandUsage

var (
	summarizeThreadHistory   = apphistory.SummarizeThreadHistory
	historyUserMessageInputs = apphistory.UserMessageInputs
	historyInputPreview      = apphistory.InputPreview
	stringPtrValue           = apphistory.StringPtrValue
)
