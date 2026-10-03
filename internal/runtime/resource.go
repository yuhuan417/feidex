package runtime

import (
	"context"
	"sync"
)

type ManagedResource interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// Resource owns an explicitly composed auxiliary server's lifecycle.
type Resource struct {
	mu      sync.Mutex
	server  ManagedResource
	started bool
}

func NewResource(server ManagedResource) *Resource { return &Resource{server: server} }

func (r *Resource) Start(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	if err := r.server.Start(ctx); err != nil {
		return err
	}
	r.started = true
	return nil
}

func (r *Resource) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return nil
	}
	r.started = false
	return r.server.Stop(ctx)
}

func (r *Resource) Started() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}
