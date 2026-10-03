package runtime

import (
	"context"
	"sync"
)

// EffectDeduper coordinates retries of the same semantic effect. A result is
// recorded only after the external operation succeeds; failed operations can
// therefore be retried safely.
type EffectDeduper interface {
	Do(context.Context, string, func() (any, error)) (any, error)
}

type effectResult struct {
	value any
	err   error
}

type effectWaiter struct {
	done   chan struct{}
	result effectResult
}

// MemoryEffectDeduper is frontend scoped and intentionally process-local. The
// domain identity in each key makes it safe for actor retries and reconnects;
// persistent repositories can implement EffectDeduper when cross-process
// replay needs to be recovered later.
type MemoryEffectDeduper struct {
	mu        sync.Mutex
	completed map[string]any
	inflight  map[string]*effectWaiter
}

func NewMemoryEffectDeduper() *MemoryEffectDeduper {
	return &MemoryEffectDeduper{completed: map[string]any{}, inflight: map[string]*effectWaiter{}}
}

func (d *MemoryEffectDeduper) Do(ctx context.Context, key string, fn func() (any, error)) (any, error) {
	if d == nil || key == "" || fn == nil {
		if fn == nil {
			return nil, nil
		}
		return fn()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d.mu.Lock()
	if value, ok := d.completed[key]; ok {
		d.mu.Unlock()
		return value, nil
	}
	if waiter, ok := d.inflight[key]; ok {
		d.mu.Unlock()
		select {
		case <-waiter.done:
			return waiter.result.value, waiter.result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	waiter := &effectWaiter{done: make(chan struct{})}
	d.inflight[key] = waiter
	d.mu.Unlock()

	value, err := fn()
	d.mu.Lock()
	delete(d.inflight, key)
	if err == nil {
		d.completed[key] = value
	}
	waiter.result = effectResult{value: value, err: err}
	close(waiter.done)
	d.mu.Unlock()
	return value, err
}
