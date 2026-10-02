package backend

import (
	"feidex/internal/app/appcore"
	"feidex/internal/state"
)

type SelectionSource interface {
	appcore.ConfigurationSource
	appcore.FrontendIdentity
	Store() *state.Store
	SetBackend(string)
	ConfigPath() string
}
