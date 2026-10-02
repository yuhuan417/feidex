package runtime

import "context"

// ManagedFrontend is the lifecycle boundary consumed by the multi-frontend
// supervisor. Product behavior lives behind its input dispatcher.
type ManagedFrontend interface {
	Prepare(context.Context) error
	StartInboundGC()
	RecoverShared()
	RecoverFrontend()
	Serve() error
	StartBackground()
	Stop(context.Context) error
}
type FrontendGroup struct{ Frontends []ManagedFrontend }

func (g FrontendGroup) Start(ctx context.Context) error {
	started := make([]ManagedFrontend, 0, len(g.Frontends))
	for _, frontend := range g.Frontends {
		// Prepare may have admitted work before failing. Include it in rollback.
		started = append(started, frontend)
		if err := frontend.Prepare(ctx); err != nil {
			_ = stopFrontends(ctx, started)
			return err
		}
	}
	for _, frontend := range g.Frontends {
		frontend.StartInboundGC()
	}
	if len(g.Frontends) > 0 {
		g.Frontends[0].RecoverShared()
	}
	for _, frontend := range g.Frontends {
		frontend.RecoverFrontend()
	}
	for _, frontend := range g.Frontends {
		if err := frontend.Serve(); err != nil {
			_ = stopFrontends(ctx, started)
			return err
		}
	}
	for _, frontend := range g.Frontends {
		frontend.StartBackground()
	}
	return nil
}
func (g FrontendGroup) Stop(ctx context.Context) error { return stopFrontends(ctx, g.Frontends) }
func stopFrontends(ctx context.Context, frontends []ManagedFrontend) error {
	var first error
	for i := len(frontends) - 1; i >= 0; i-- {
		if err := frontends[i].Stop(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
