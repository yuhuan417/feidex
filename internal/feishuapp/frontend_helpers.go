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

func startFrontend(client FeishuClient, ctx context.Context) error {
	if client == nil {
		return nil
	}
	return client.Start(ctx)
}
