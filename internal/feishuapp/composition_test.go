package feishuapp

import (
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
	"fmt"
	"path/filepath"
	"sync"
)

func New(cfg *config.Config, path string) (*App, error) {
	svc, err := newTestService(cfg, path)
	if err != nil || len(svc) == 0 {
		return nil, err
	}
	return svc[0], nil
}

func newTestFrontend(scope frontendruntime.FrontendScope) (*App, error) {
	scope.FeishuTransport = appfeishuwrap.WrapFeishuClient(newFeishuClient(scope.Frontend.Feishu))
	scope.Registry = frontendruntime.NewRegistry(scope.FeishuTransport)
	scope.RuntimeOwner = frontendruntime.NewFrontendOwner()
	scope.InboundDeduper = frontendruntime.NewInboundDeduper()
	a, err := NewFeishuShell(scope)
	if err != nil {
		return nil, err
	}
	AttachTrackers(a, NewTrackers(a))
	AttachEffectRunner(a, NewEffectRunner(a))
	AttachStateView(a, NewStateView(a))
	AttachWorkspacePresentation(a, NewWorkspacePresentation(a))
	AttachDispatcher(a, NewDispatcher(a))
	if err := CanonicalizeStoredSessionKeys(a); err != nil {
		return nil, err
	}
	if backend := BackendKind(a); backend != "" {
		handle, err := BuildBackendRuntimeHandle(a, backend)
		if err != nil {
			return nil, err
		}
		InstallBackendRuntime(a, handle)
	}
	InstallFeishuPolicies(a)
	a.feishu.SetHandlers(a.HandleFeishuMessage, a.HandleCardAction, a.HandleFeishuRecall, a.HandleFeishuReaction)
	a.feishu.ConfigureLocalFileLinks("", "")
	return a, nil
}

func newTestService(cfg *config.Config, path string) ([]*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	frontends := cfg.ResolvedFrontends()
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	mu := &sync.RWMutex{}
	apps := make([]*App, 0, len(frontends))
	for _, frontend := range frontends {
		a, err := newTestFrontend(frontendruntime.FrontendScope{Config: cfg, ConfigPath: path, Store: store, ConfigMutex: mu, Frontend: frontend})
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}
