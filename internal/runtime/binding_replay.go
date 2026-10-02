package runtime

import (
	"context"
	"feidex/internal/application/routing"
)

// BindingReplay serializes a binding's pending queue and acknowledges its head
// only after the enqueue effect succeeds. A failed enqueue retains that head.
type BindingReplay struct {
	Service routing.PendingService
	Runner  EffectRunner
	Actors  *SessionActors
}

func (w BindingReplay) Replay(ctx context.Context, bindingID string) error {
	var result error
	run := func() {
		for {
			if err := ctx.Err(); err != nil {
				result = err
				return
			}
			next := w.Service.Next(bindingID)
			if !next.Exists {
				return
			}
			if err := w.Runner.Run(ctx, next.Effects); err != nil {
				result = err
				return
			}
			effects, err := w.Service.Acknowledge(bindingID, next.Pending)
			if err != nil {
				result = err
				return
			}
			if err := w.Runner.Run(ctx, effects); err != nil {
				result = err
				return
			}
		}
	}
	if w.Actors == nil {
		run()
	} else {
		w.Actors.Run("binding-replay:"+bindingID, run)
	}
	return result
}
