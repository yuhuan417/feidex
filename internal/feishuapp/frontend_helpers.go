package feishuapp

import (
	"context"
)

func startBackend(a *App, ctx context.Context) error {
	if a == nil {
		return nil
	}
	return startPreparedBackendRuntime(a, ctx, currentBackendRuntimeHandle(a))
}

func startFrontend(a *App, ctx context.Context) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.Start(ctx)
}

func allowLegacyFrontendFallback(a *App) bool {
	if a == nil || a.cfg == nil {
		return false
	}
	a.configMutex().RLock()
	defer a.configMutex().RUnlock()
	return len(a.cfg.ResolvedFrontends()) == 1
}
