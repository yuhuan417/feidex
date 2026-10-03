package backend

import (
	"context"
	"feidex/internal/config"
	"feidex/internal/state"
	"sync"
)

type SelectionSource interface {
	Config() *config.Config
	ConfigMu() *sync.RWMutex
	Backend() string
	FrontendConfigIndex() int
	FrontendID() string
	Context() context.Context
	Store() *state.Store
	SetBackend(string)
	ConfigPath() string
}
