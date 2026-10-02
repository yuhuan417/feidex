package backend

import "feidex/internal/app/appcore"

type SelectionSource interface {
	appcore.WorkspaceSource
	SetBackend(string)
	ConfigPath() string
}
