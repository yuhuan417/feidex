package runtime

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	DedupRetention  = 10 * time.Minute
	InflightTTL     = 2 * time.Minute
	DedupGCInterval = time.Minute
	DedupMaxEntries = 4096
)

type InboundDeduper struct {
	Mu           sync.Mutex
	Inflight     map[string]time.Time
	RecentlyDone map[string]time.Time
	Retention    time.Duration
	InflightTTL  time.Duration
	MaxEntries   int
}

func NewInboundDeduper() *InboundDeduper {
	return &InboundDeduper{
		Inflight:     map[string]time.Time{},
		RecentlyDone: map[string]time.Time{},
		Retention:    DedupRetention,
		InflightTTL:  InflightTTL,
		MaxEntries:   DedupMaxEntries,
	}
}

func (d *InboundDeduper) Claim(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if d == nil || messageID == "" {
		return true
	}
	now := time.Now()
	d.Mu.Lock()
	defer d.Mu.Unlock()
	d.pruneLocked(now)
	if expiry, ok := d.RecentlyDone[messageID]; ok && expiry.After(now) {
		return false
	}
	delete(d.RecentlyDone, messageID)
	if expiry, ok := d.Inflight[messageID]; ok && expiry.After(now) {
		return false
	}
	delete(d.Inflight, messageID)
	d.Inflight[messageID] = now.Add(d.InflightTTL)
	d.enforceCapLocked()
	return true
}

func (d *InboundDeduper) MarkDone(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if d == nil || messageID == "" {
		return
	}
	now := time.Now()
	d.Mu.Lock()
	defer d.Mu.Unlock()
	delete(d.Inflight, messageID)
	d.RecentlyDone[messageID] = now.Add(d.Retention)
	d.pruneLocked(now)
	d.enforceCapLocked()
}

func (d *InboundDeduper) Release(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if d == nil || messageID == "" {
		return
	}
	d.Mu.Lock()
	defer d.Mu.Unlock()
	delete(d.Inflight, messageID)
}

func (d *InboundDeduper) RunGC(ctx context.Context) {
	if d == nil {
		return
	}
	ticker := time.NewTicker(DedupGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.GC()
		}
	}
}

func (d *InboundDeduper) GC() {
	if d == nil {
		return
	}
	now := time.Now()
	d.Mu.Lock()
	defer d.Mu.Unlock()
	d.pruneLocked(now)
}

func (d *InboundDeduper) pruneLocked(now time.Time) {
	for id, expiry := range d.Inflight {
		if !expiry.After(now) {
			delete(d.Inflight, id)
		}
	}
	for id, expiry := range d.RecentlyDone {
		if !expiry.After(now) {
			delete(d.RecentlyDone, id)
		}
	}
}

func (d *InboundDeduper) enforceCapLocked() {
	if d.MaxEntries <= 0 {
		return
	}
	for len(d.Inflight)+len(d.RecentlyDone) > d.MaxEntries {
		oldestID := ""
		oldestExpiry := time.Time{}
		pick := func(id string, expiry time.Time) {
			if oldestID == "" || expiry.Before(oldestExpiry) {
				oldestID = id
				oldestExpiry = expiry
			}
		}
		for id, expiry := range d.Inflight {
			pick(id, expiry)
		}
		for id, expiry := range d.RecentlyDone {
			pick(id, expiry)
		}
		if oldestID == "" {
			return
		}
		delete(d.Inflight, oldestID)
		delete(d.RecentlyDone, oldestID)
	}
}
