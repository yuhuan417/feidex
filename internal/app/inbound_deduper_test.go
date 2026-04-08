package app

import (
	"context"
	"testing"
	"time"
)

func TestInboundDeduperClaimReleaseAndDone(t *testing.T) {
	d := &inboundDeduper{
		inflight:     map[string]time.Time{},
		recentlyDone: map[string]time.Time{},
		retention:    50 * time.Millisecond,
		inflightTTL:  50 * time.Millisecond,
		maxEntries:   8,
	}

	if !d.Claim("msg-1") {
		t.Fatal("first Claim(msg-1) should succeed")
	}
	if d.Claim("msg-1") {
		t.Fatal("second Claim(msg-1) should be blocked while inflight")
	}

	d.Release("msg-1")
	if !d.Claim("msg-1") {
		t.Fatal("Claim(msg-1) should succeed after Release")
	}

	d.MarkDone("msg-1")
	if d.Claim("msg-1") {
		t.Fatal("Claim(msg-1) should be blocked while recently done")
	}

	time.Sleep(70 * time.Millisecond)
	if !d.Claim("msg-1") {
		t.Fatal("Claim(msg-1) should succeed after retention expiry")
	}
}

func TestInboundDeduperGCAndCap(t *testing.T) {
	d := &inboundDeduper{
		inflight:     map[string]time.Time{},
		recentlyDone: map[string]time.Time{},
		retention:    time.Second,
		inflightTTL:  time.Second,
		maxEntries:   2,
	}

	if !d.Claim("msg-1") || !d.Claim("msg-2") || !d.Claim("msg-3") {
		t.Fatal("Claim() should allow unique ids")
	}
	if got := len(d.inflight) + len(d.recentlyDone); got != 2 {
		t.Fatalf("enforceCapLocked() total = %d, want 2", got)
	}

	d.mu.Lock()
	d.inflight["expired"] = time.Now().Add(-time.Second)
	d.mu.Unlock()
	d.gc()
	d.mu.Lock()
	if _, ok := d.inflight["expired"]; ok {
		d.mu.Unlock()
		t.Fatal("gc() should remove expired inflight entry")
	}
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx)
	cancel()
}
