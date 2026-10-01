package app

import (
	"context"
)

func startInboundDeduperLoop(a *App, ctx context.Context) {
	if a == nil || a.deduper == nil {
		return
	}
	a.deduper.Start(ctx)
}
