package maintenance

import (
	"context"
	"time"
)

// StartPeriodic uses frontend task admission for both the first pass and loop.
func StartPeriodic(ctx context.Context, admit func(func()), interval time.Duration, run func(string)) {
	admit(func() {
		if ctx.Err() == nil {
			run("startup")
		}
	})
	admit(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run("ticker")
			}
		}
	})
}
