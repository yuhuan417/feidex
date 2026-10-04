package feishuapp

import (
	"context"
)

func startFrontend(client FeishuClient, ctx context.Context) error {
	if client == nil {
		return nil
	}
	return client.Start(ctx)
}
