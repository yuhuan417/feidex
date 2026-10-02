package runtime

import (
	"context"
	"testing"
	"time"
)

func TestFrontendRuntimeCancelsAndWaits(t *testing.T) {
	var r FrontendRuntime
	r.Begin(context.Background())
	started, finished := make(chan struct{}), make(chan struct{})
	ctx := r.Context()
	if !r.Run(func() { close(started); <-ctx.Done(); close(finished) }, nil) {
		t.Fatal("Run rejected active runtime")
	}
	<-started
	r.Cancel()
	shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Wait returned before work exited")
	}
	if r.Run(func() {}, nil) {
		t.Fatal("Run admitted work after Cancel")
	}
}

func TestFrontendRuntimeParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	var r FrontendRuntime
	r.Begin(parent)
	cancel()
	if r.Context().Err() != context.Canceled {
		t.Fatal("parent cancellation not propagated")
	}
	if r.Run(func() { t.Error("admitted work after parent cancellation") }, nil) {
		t.Fatal("Run should reject work")
	}
}

func TestFrontendRuntimeShutdownDeadlineIncludesUndispatchedWork(t *testing.T) {
	var r FrontendRuntime
	var queued func()
	r.Run(func() {}, func(job func()) { queued = job })
	r.Cancel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Wait(ctx); err != context.Canceled {
		t.Fatalf("Wait = %v, want cancellation while job is queued", err)
	}
	queued()
	if err := r.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
