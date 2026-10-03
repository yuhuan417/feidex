package runtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// CoalescedRefresh owns timers and concurrent refresh admission for one frontend.
type CoalescedRefresh struct {
	mu                 sync.Mutex
	debounce, interval time.Duration
	entries            map[string]*refreshEntry
	stopped            bool
	lifecycle          *FrontendRuntime
	refresh            func(context.Context, string) error
}

type refreshEntry struct {
	timer           *time.Timer
	running, queued bool
	last            time.Time
}

func NewCoalescedRefresh(lifecycle *FrontendRuntime, debounce, interval time.Duration, refresh func(context.Context, string) error) *CoalescedRefresh {
	return &CoalescedRefresh{lifecycle: lifecycle, debounce: debounce, interval: interval, refresh: refresh, entries: map[string]*refreshEntry{}}
}

func (r *CoalescedRefresh) Schedule(key string) {
	key = strings.TrimSpace(key)
	if r == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	e := r.entries[key]
	if e == nil {
		e = &refreshEntry{}
		r.entries[key] = e
	}
	if e.running {
		e.queued = true
		return
	}
	delay := max(r.debounce, 0)
	if !e.last.IsZero() {
		delay = max(delay, r.interval-time.Since(e.last))
	}
	if e.timer != nil {
		e.timer.Stop()
	}
	e.timer = time.AfterFunc(delay, func() { r.execute(key, e) })
}

func (r *CoalescedRefresh) execute(key string, e *refreshEntry) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	if e.running {
		e.queued = true
		r.mu.Unlock()
		return
	}
	e.timer = nil
	e.running, e.queued, e.last = true, false, time.Now()
	r.mu.Unlock()
	if !r.lifecycle.Run(func() {
		ctx, cancel := context.WithTimeout(r.lifecycle.Context(), 20*time.Second)
		err := r.refresh(ctx, key)
		cancel()
		if err != nil {
			slog.Warn("frontend refresh failed", "key", key, "error", err)
		}
		r.finish(key, e)
	}, nil) {
		r.finish(key, e)
	}
}

func (r *CoalescedRefresh) finish(key string, e *refreshEntry) {
	r.mu.Lock()
	queued := e.queued
	e.running, e.queued = false, false
	r.mu.Unlock()
	if queued {
		r.Schedule(key)
	}
}

func (r *CoalescedRefresh) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	for _, e := range r.entries {
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
	}
}
