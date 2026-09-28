package app

import (
	"context"
	"testing"
	"time"

	"feidex/internal/app/appcore"
)

func TestStopCancelsAndWaitsForBackgroundWork(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.beginLifecycle(context.Background())
	ctx := appcore.Context(submissionAppAdapter{app: a})
	started, finished := make(chan struct{}), make(chan struct{})
	runAsync(a, func() { close(started); <-ctx.Done(); close(finished) })
	<-started
	shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Stop(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Stop returned before background work exited")
	}
	runAsync(a, func() { t.Error("work admitted after shutdown") })
}

func TestFrontendLifecycleCancellationIsIsolated(t *testing.T) {
	a, _, _ := newTestApp(t)
	b, _, _ := newTestApp(t)
	a.beginLifecycle(context.Background())
	b.beginLifecycle(context.Background())
	defer b.lifecycleCancel()
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.Context().Err() == nil {
		t.Fatal("stopped frontend still active")
	}
	if b.Context().Err() != nil {
		t.Fatal("other frontend canceled")
	}
}

func TestLifecycleUsesParentCancellation(t *testing.T) {
	a, _, _ := newTestApp(t)
	parent, cancel := context.WithCancel(context.Background())
	a.beginLifecycle(parent)
	cancel()
	if a.Context().Err() != context.Canceled {
		t.Fatal("parent cancellation not propagated")
	}
}
