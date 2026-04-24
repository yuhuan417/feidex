package app

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	inboundDedupRetention  = 10 * time.Minute
	inboundInflightTTL     = 2 * time.Minute
	inboundDedupGCInterval = time.Minute
	inboundDedupMaxEntries = 4096
)

type inboundDeduper struct {
	mu           sync.Mutex
	inflight     map[string]time.Time
	recentlyDone map[string]time.Time
	retention    time.Duration
	inflightTTL  time.Duration
	maxEntries   int
}

func newInboundDeduper() *inboundDeduper {
	return &inboundDeduper{
		inflight:     map[string]time.Time{},
		recentlyDone: map[string]time.Time{},
		retention:    inboundDedupRetention,
		inflightTTL:  inboundInflightTTL,
		maxEntries:   inboundDedupMaxEntries,
	}
}

func (d *inboundDeduper) Claim(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if d == nil || messageID == "" {
		return true
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(now)
	if expiry, ok := d.recentlyDone[messageID]; ok && expiry.After(now) {
		return false
	}
	delete(d.recentlyDone, messageID)
	if expiry, ok := d.inflight[messageID]; ok && expiry.After(now) {
		return false
	}
	delete(d.inflight, messageID)
	d.inflight[messageID] = now.Add(d.inflightTTL)
	d.enforceCapLocked()
	return true
}

func (d *inboundDeduper) MarkDone(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if d == nil || messageID == "" {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inflight, messageID)
	d.recentlyDone[messageID] = now.Add(d.retention)
	d.pruneLocked(now)
	d.enforceCapLocked()
}

func (d *inboundDeduper) Release(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if d == nil || messageID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inflight, messageID)
}

func (d *inboundDeduper) Start(ctx context.Context) {
	if d == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(inboundDedupGCInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.gc()
			}
		}
	}()
}

func (d *inboundDeduper) gc() {
	if d == nil {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(now)
}

func (d *inboundDeduper) pruneLocked(now time.Time) {
	for id, expiry := range d.inflight {
		if !expiry.After(now) {
			delete(d.inflight, id)
		}
	}
	for id, expiry := range d.recentlyDone {
		if !expiry.After(now) {
			delete(d.recentlyDone, id)
		}
	}
}

func (d *inboundDeduper) enforceCapLocked() {
	if d.maxEntries <= 0 {
		return
	}
	for len(d.inflight)+len(d.recentlyDone) > d.maxEntries {
		oldestID := ""
		oldestExpiry := time.Time{}
		pick := func(id string, expiry time.Time) {
			if oldestID == "" || expiry.Before(oldestExpiry) {
				oldestID = id
				oldestExpiry = expiry
			}
		}
		for id, expiry := range d.inflight {
			pick(id, expiry)
		}
		for id, expiry := range d.recentlyDone {
			pick(id, expiry)
		}
		if oldestID == "" {
			return
		}
		delete(d.inflight, oldestID)
		delete(d.recentlyDone, oldestID)
	}
}

func startInboundDeduperLoop(a *App, ctx context.Context) {
	if a == nil || a.deduper == nil {
		return
	}
	a.deduper.Start(ctx)
}
