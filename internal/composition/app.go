// Package composition constructs frontend scopes and their runtime supervisor.
// Factories assemble concrete adapters; no input, menu, or product policy lives here.
package composition

import (
	"fmt"
	"path/filepath"
	"sync"

	"feidex/internal/config"
	"feidex/internal/runtime"
	"feidex/internal/state"
)

type FrontendScope struct {
	Config      *config.Config
	ConfigPath  string
	Store       *state.Store
	ConfigMutex *sync.RWMutex
	Frontend    config.ResolvedFrontend
}

// NewFrontendOwner is the composition boundary for frontend-scoped mutable
// runtime state. The owner itself lives in runtime; composition decides when
// one is created for each frontend.
func NewFrontendOwner() *runtime.FrontendOwner {
	return runtime.NewFrontendOwner()
}

type Factory[T runtime.ManagedFrontend] func(FrontendScope) (T, error)
type Service[T runtime.ManagedFrontend] struct {
	runtime.FrontendGroup
	Frontends []T
}

func scopes(cfg *config.Config, cfgPath string) ([]FrontendScope, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	frontends := cfg.ResolvedFrontends()
	if len(frontends) == 0 {
		return nil, fmt.Errorf("no frontend configured")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	mu := &sync.RWMutex{}
	result := make([]FrontendScope, 0, len(frontends))
	for _, frontend := range frontends {
		result = append(result, FrontendScope{Config: cfg, ConfigPath: cfgPath, Store: store, ConfigMutex: mu, Frontend: frontend})
	}
	return result, nil
}
func NewService[T runtime.ManagedFrontend](cfg *config.Config, cfgPath string, factory Factory[T]) (*Service[T], error) {
	inputs, err := scopes(cfg, cfgPath)
	if err != nil {
		return nil, err
	}
	service := &Service[T]{}
	for _, input := range inputs {
		frontend, err := factory(input)
		if err != nil {
			return nil, err
		}
		service.Frontends = append(service.Frontends, frontend)
		service.FrontendGroup.Frontends = append(service.FrontendGroup.Frontends, frontend)
	}
	return service, nil
}
func New[T runtime.ManagedFrontend](cfg *config.Config, cfgPath string, factory Factory[T]) (T, error) {
	var zero T
	inputs, err := scopes(cfg, cfgPath)
	if err != nil {
		return zero, err
	}
	return factory(inputs[0])
}
