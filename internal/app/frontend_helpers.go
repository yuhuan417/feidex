package app

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
