package feishuapp

import (
	"sync"

	appstate "feidex/internal/adapter/storage/json/scoped"

	"feidex/internal/config"
)

// Feishu returns the Feishu client. Sub-packages should define narrow
// interfaces for the methods they need rather than depending on this type.
func (a *Frontend) Feishu() FeishuClient {
	if a == nil {
		return nil
	}
	return a.feishu
}

// Config returns the application configuration.
func (a *Frontend) Config() *config.Config {
	if a == nil {
		return nil
	}
	return a.cfg
}

// State returns the frontend-scoped app state store.
func (a *Frontend) State() *appstate.Store {
	if a == nil {
		return nil
	}
	return a.stateView
}

// FrontendID returns the configured frontend identifier.
func (a *Frontend) FrontendID() string {
	if a == nil {
		return ""
	}
	return a.frontendID
}

// ConfigMu returns the config read-write mutex.
func (a *Frontend) ConfigMu() *sync.RWMutex {
	if a == nil {
		return nil
	}
	return a.configMutex()
}
