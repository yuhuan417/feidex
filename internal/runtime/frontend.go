// Package runtime owns frontend lifecycle and background task execution.
package runtime

import (
	"context"
	"sync"
)

// FrontendRuntime owns cancellation and admission of background work for one frontend.
// Its zero value accepts work, so entrypoints can be used before startup.
// Begin must only be called after work from any previous lifecycle has drained.
type FrontendRuntime struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	stopping bool
	workers  sync.WaitGroup
}

func (r *FrontendRuntime) Begin(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	r.stopping = false
}

func (r *FrontendRuntime) Context() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// Cancel closes admission before cancelling work. Transport cleanup uses a
// separate shutdown context and can proceed after this method returns.
func (r *FrontendRuntime) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopping = true
	if r.cancel != nil {
		r.cancel()
	}
}

// Run admits a job before dispatching it. An injected runner must invoke the job
// exactly once; nil uses a goroutine. Admission and shutdown are serialized.
func (r *FrontendRuntime) Run(job func(), runner func(func())) bool {
	if job == nil {
		return false
	}
	r.mu.Lock()
	if r.stopping || (r.ctx != nil && r.ctx.Err() != nil) {
		r.mu.Unlock()
		return false
	}
	r.workers.Add(1)
	r.mu.Unlock()
	wrapped := func() { defer r.workers.Done(); job() }
	if runner != nil {
		runner(wrapped)
	} else {
		go wrapped()
	}
	return true
}

// Wait drains admitted work up to the shutdown deadline. Cancel must precede
// Wait so no new work can race with an empty worker group.
func (r *FrontendRuntime) Wait(shutdown context.Context) error {
	if shutdown == nil {
		shutdown = context.Background()
	}
	done := make(chan struct{})
	go func() { r.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-shutdown.Done():
		return shutdown.Err()
	}
}
