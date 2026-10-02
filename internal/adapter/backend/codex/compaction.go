package codex

import (
	"context"
	"fmt"
)

type CompactionGateway struct {
	Client interface {
		Call(context.Context, string, any, any) error
	}
}

func (g CompactionGateway) StartCompaction(ctx context.Context, id string) error {
	if g.Client == nil {
		return fmt.Errorf("codex client not initialized")
	}
	return g.Client.Call(ctx, "thread/compact/start", map[string]any{"threadId": id}, nil)
}
