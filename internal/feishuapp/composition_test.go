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

func New(cfg *config.Config, path string) (*Frontend, error) {
	svc, err := newTestService(cfg, path)
	if err != nil || len(svc) == 0 {
		return nil, err
	}
	return svc[0], nil
}

func newTestFrontend(scope frontendruntime.FrontendScope) (*Frontend, error) {
	scope.FeishuTransport = appfeishuwrap.WrapFeishuClient(newFeishuClient(scope.Frontend.Feishu))
	scope.RuntimeOwner = frontendruntime.NewFrontendOwner()
	a, err := NewFeishuShell(scope)
	if err != nil {
		return nil, err
	}
	prepareTestApp(a)
	a.feishu = AttachEffectRuntime(a.runtimeOwner, a.feishu, a.frontendID, NewEffectRunner(testEffectRunnerInputs(a)))
	a.bindings.WorkspacePresentation = testWorkspacePresentation(a)
	dispatcher := NewDispatcher(testDispatcherInputs(a))
	a.runtimeOwner.Dispatcher = &dispatcher
	if err := CanonicalizeStoredSessionKeys(a.store); err != nil {
		return nil, err
	}
	if backend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.frontendConfigIndex)(); backend != "" {
		handle, err := BuildBackendRuntimeHandle(a.BackendRuntimeDeps(), backend)
		if err != nil {
			return nil, err
		}
		InstallBackendRuntime(a.BackendRuntimeDeps(), handle)
	}
	InstallFeishuPolicies(testFeishuPolicyInputs(a))
	a.feishu.SetHandlers(a.HandleFeishuMessage, a.HandleCardAction, a.HandleFeishuRecall, a.HandleFeishuReaction)
	a.feishu.ConfigureLocalFileLinks("", "")
	return a, nil
}

func newTestService(cfg *config.Config, path string) ([]*Frontend, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	frontends := cfg.ResolvedFrontends()
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	mu := &sync.RWMutex{}
	apps := make([]*Frontend, 0, len(frontends))
	for _, frontend := range frontends {
		a, err := newTestFrontend(frontendruntime.FrontendScope{Config: cfg, ConfigPath: path, Store: store, ConfigMutex: mu, Frontend: frontend})
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}
