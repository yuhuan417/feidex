package app

import (
	apphistorycmd "feidex/internal/app/historycmd"
	apphistory "feidex/internal/app/apphistory"
)

// ---------------------------------------------------------------------------
// Type and constructor aliases — historycmd sub-package
// ---------------------------------------------------------------------------

type historyService = apphistorycmd.Service

func newHistoryService(app *App) historyService {
	return apphistorycmd.NewService(app)
}

// ---------------------------------------------------------------------------
// Constant aliases
// ---------------------------------------------------------------------------

const historyPageSize = apphistorycmd.HistoryPageSize
const historyCommandUsage = apphistorycmd.HistoryCommandUsage

// ---------------------------------------------------------------------------
// Function aliases for apphistory (kept for backward compatibility)
// ---------------------------------------------------------------------------

var (
	summarizeThreadHistory   = apphistory.SummarizeThreadHistory
	historyUserMessageInputs = apphistory.UserMessageInputs
	historyInputPreview      = apphistory.InputPreview
	stringPtrValue           = apphistory.StringPtrValue
)
