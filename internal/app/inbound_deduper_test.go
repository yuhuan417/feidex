package app

import (
	"context"
	"testing"
	"time"
)

func TestInboundDeduperClaimReleaseAndDone(t *testing.T) {
	d := &inboundDeduper{
		Inflight:     map[string]time.Time{},
		RecentlyDone: map[string]time.Time{},
		Retention:    50 * time.Millisecond,
		InflightTTL:  50 * time.Millisecond,
		MaxEntries:   8,
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
		Inflight:     map[string]time.Time{},
		RecentlyDone: map[string]time.Time{},
		Retention:    time.Second,
		InflightTTL:  time.Second,
		MaxEntries:   2,
	}

	if !d.Claim("msg-1") || !d.Claim("msg-2") || !d.Claim("msg-3") {
		t.Fatal("Claim() should allow unique ids")
	}
	if got := len(d.Inflight) + len(d.RecentlyDone); got != 2 {
		t.Fatalf("enforceCapLocked() total = %d, want 2", got)
	}

	d.Mu.Lock()
	d.Inflight["expired"] = time.Now().Add(-time.Second)
	d.Mu.Unlock()
	d.GC()
	d.Mu.Lock()
	if _, ok := d.Inflight["expired"]; ok {
		d.Mu.Unlock()
		t.Fatal("gc() should remove expired Inflight entry")
	}
	d.Mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx)
	cancel()
}
