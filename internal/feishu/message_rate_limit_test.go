package feishu

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Feishu rejects im.message.patch beyond 5 updates per second for a single
// message with code 230020. Pacing must keep a margin below that, since a pacer
// sitting exactly on the limit still trips when request timing jitters inside
// the platform's window.
func TestMessagePatchPacerKeepsMarginBelowFeishuLimit(t *testing.T) {
	const feishuPatchLimitPerSecond = 5

	limitInterval := time.Second / time.Duration(feishuPatchLimitPerSecond)
	if interval := time.Second / time.Duration(feishuMessagePatchQPS); interval <= limitInterval {
		t.Fatalf("patch interval = %v, must be above the %v implied by Feishu's %d QPS limit",
			interval, limitInterval, feishuPatchLimitPerSecond)
	}
}

func TestRequestPacerWaitsBetweenCalls(t *testing.T) {
	pacer := newRequestPacerWithInterval(25 * time.Millisecond)

	if delay, err := pacer.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	} else if delay != 0 {
		t.Fatalf("first Wait() delay = %v, want 0", delay)
	}

	start := time.Now()
	delay, err := pacer.Wait(context.Background())
	if err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if delay < 20*time.Millisecond {
		t.Fatalf("second Wait() delay = %v, want >= 20ms", delay)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("second Wait() elapsed = %v, want >= 20ms", elapsed)
	}
}

func TestRequestPacerHonorsContextCancellation(t *testing.T) {
	pacer := newRequestPacerWithInterval(40 * time.Millisecond)
	if _, err := pacer.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	if _, err := pacer.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() error = %v, want deadline exceeded", err)
	}
}

func TestKeyedRequestPacerScopesByKey(t *testing.T) {
	pacer := newKeyedRequestPacerWithInterval(25*time.Millisecond, time.Minute, time.Hour)

	if _, err := pacer.Wait(context.Background(), "msg-1"); err != nil {
		t.Fatalf("Wait(msg-1) error = %v", err)
	}

	startOther := time.Now()
	delayOther, err := pacer.Wait(context.Background(), "msg-2")
	if err != nil {
		t.Fatalf("Wait(msg-2) error = %v", err)
	}
	if delayOther != 0 {
		t.Fatalf("Wait(msg-2) delay = %v, want 0", delayOther)
	}
	if elapsedOther := time.Since(startOther); elapsedOther >= 15*time.Millisecond {
		t.Fatalf("Wait(msg-2) elapsed = %v, want < 15ms", elapsedOther)
	}

	startSame := time.Now()
	delaySame, err := pacer.Wait(context.Background(), "msg-1")
	if err != nil {
		t.Fatalf("second Wait(msg-1) error = %v", err)
	}
	if delaySame < 20*time.Millisecond {
		t.Fatalf("second Wait(msg-1) delay = %v, want >= 20ms", delaySame)
	}
	if elapsedSame := time.Since(startSame); elapsedSame < 20*time.Millisecond {
		t.Fatalf("second Wait(msg-1) elapsed = %v, want >= 20ms", elapsedSame)
	}
}
