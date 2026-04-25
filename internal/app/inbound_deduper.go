package app

import (
	"context"

	appinbounddedup "feidex/internal/app/inbounddedup"
)

type inboundDeduper = appinbounddedup.Deduper

var newInboundDeduper = appinbounddedup.NewDeduper

func startInboundDeduperLoop(a *App, ctx context.Context) {
	if a == nil || a.deduper == nil {
		return
	}
	a.deduper.Start(ctx)
}
