// Package composition is the production composition root.
package composition

import (
	"fmt"
	"path/filepath"
	"sync"

	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/app"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
	"feidex/internal/state"
)

type FrontendScope = runtime.FrontendScope

type Factory[T runtime.ManagedFrontend] func(FrontendScope) (T, error)
type Service[T runtime.ManagedFrontend] struct {
	runtime.FrontendGroup
	Frontends []T
}

func NewFrontend(scope FrontendScope) (*app.App, error) {
	frontend, err := app.NewFeishuShell(scope)
	if err != nil {
		return nil, err
	}
	// All production objects are assembled here. internal/app only exposes
	// boundary factories and attaches the resulting parts to its Feishu shell.
	app.AttachTrackers(frontend, app.NewTrackers(frontend))
	app.AttachEffectRunner(frontend, app.NewEffectRunner(frontend))
	app.AttachStateView(frontend, app.NewStateView(frontend))
	app.AttachWorkspacePresentation(frontend, app.NewWorkspacePresentation(frontend))
	app.AttachDispatcher(frontend, app.NewDispatcher(frontend))
	if err := app.CanonicalizeStoredSessionKeys(frontend); err != nil {
		return nil, err
	}
	if backend := app.BackendKind(frontend); backend != "" {
		handle, err := app.BuildBackendRuntimeHandle(frontend, backend)
		if err != nil {
			return nil, err
		}
		app.InstallBackendRuntime(frontend, handle)
	}
	app.InstallFeishuHandlers(frontend)
	return frontend, nil
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
		transport := appfeishuwrap.WrapFeishuClient(feishu.New(frontend.Feishu))
		result = append(result, FrontendScope{
			Config: cfg, ConfigPath: cfgPath, Store: store, ConfigMutex: mu, Frontend: frontend,
			FeishuTransport: transport,
			Registry:        runtime.NewRegistry(transport),
			RuntimeOwner:    runtime.NewFrontendOwner(),
			InboundDeduper:  runtime.NewInboundDeduper(),
		})
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
