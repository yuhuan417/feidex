package feishuapp

import (
	"sync"

	"feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/state"
)

type frontendConfigProvider struct {
	config              *config.Config
	configMu            *sync.RWMutex
	backend             func() string
	frontendID          string
	frontendConfigIndex int
	configPath          string
	store               *state.Store
	workspaceSelection  workspace.SelectionService
}

func newFrontendConfigProvider(runtime BackendRuntimeDeps, store *state.Store, selection workspace.SelectionService) frontendConfigProvider {
	backend := runtime.view.configuredBackend
	if runtime.runtime.owner != nil {
		backend = runtime.runtime.owner.Backend
	}
	return frontendConfigProvider{
		config: runtime.cfg, configMu: runtime.view.mu, backend: backend,
		frontendID: runtime.frontendID, frontendConfigIndex: runtime.view.frontendConfigIndex,
		configPath: runtime.cfgPath, store: store, workspaceSelection: selection,
	}
}

func (p frontendConfigProvider) Config() *config.Config  { return p.config }
func (p frontendConfigProvider) ConfigMu() *sync.RWMutex { return p.configMu }
func (p frontendConfigProvider) Backend() string         { return p.backend() }
func (p frontendConfigProvider) FrontendID() string      { return p.frontendID }
func (p frontendConfigProvider) FrontendConfigIndex() int {
	return p.frontendConfigIndex
}
func (p frontendConfigProvider) ConfigPath() string  { return p.configPath }
func (p frontendConfigProvider) Store() *state.Store { return p.store }
func (p frontendConfigProvider) WorkspaceSelection() workspace.SelectionService {
	return p.workspaceSelection
}
