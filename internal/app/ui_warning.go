package app

import appconvbackend "feidex/internal/app/convbackend"

type uiWarningError = appconvbackend.UIWarningError

var (
	newUIWarningError = appconvbackend.NewUIWarningError
	isUIWarningError  = appconvbackend.IsUIWarningError
)
