package feishu

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	feishuMessageCreateQPS  = 5
	feishuMessagePatchQPS   = 5
	keyedPacerIdleTTL       = 30 * time.Minute
	keyedPacerSweepInterval = 5 * time.Minute
)

type requestPacer struct {
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

func newRequestPacer(qps int) *requestPacer {
	if qps <= 0 {
		return &requestPacer{}
	}
	return newRequestPacerWithInterval(time.Second / time.Duration(qps))
}

func newRequestPacerWithInterval(interval time.Duration) *requestPacer {
	return &requestPacer{interval: interval}
}

func (p *requestPacer) Wait(ctx context.Context) (time.Duration, error) {
	if p == nil || p.interval <= 0 {
		return 0, nil
	}
	slot := p.reserve(time.Now())
	delay := time.Until(slot)
	if delay <= 0 {
		return 0, nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return delay, nil
	}
}

func (p *requestPacer) reserve(now time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	slot := now
	if p.next.After(slot) {
		slot = p.next
	}
	p.next = slot.Add(p.interval)
	return slot
}

type keyedRequestPacer struct {
	interval      time.Duration
	idleTTL       time.Duration
	sweepInterval time.Duration

	mu        sync.Mutex
	lastSweep time.Time
	entries   map[string]*keyedRequestPacerEntry
}

type keyedRequestPacerEntry struct {
	pacer    *requestPacer
	lastUsed time.Time
}

func newKeyedRequestPacer(qps int) *keyedRequestPacer {
	if qps <= 0 {
		return &keyedRequestPacer{}
	}
	return newKeyedRequestPacerWithInterval(time.Second/time.Duration(qps), keyedPacerIdleTTL, keyedPacerSweepInterval)
}

func newKeyedRequestPacerWithInterval(interval, idleTTL, sweepInterval time.Duration) *keyedRequestPacer {
	return &keyedRequestPacer{
		interval:      interval,
		idleTTL:       idleTTL,
		sweepInterval: sweepInterval,
		entries:       map[string]*keyedRequestPacerEntry{},
	}
}

func (p *keyedRequestPacer) Wait(ctx context.Context, key string) (time.Duration, error) {
	if p == nil || p.interval <= 0 {
		return 0, nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, nil
	}
	return p.entryFor(key).Wait(ctx)
}

func (p *keyedRequestPacer) entryFor(key string) *requestPacer {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string]*keyedRequestPacerEntry{}
	}
	if p.shouldSweep(now) {
		p.sweep(now)
	}
	entry := p.entries[key]
	if entry == nil {
		entry = &keyedRequestPacerEntry{pacer: newRequestPacerWithInterval(p.interval)}
		p.entries[key] = entry
	}
	entry.lastUsed = now
	return entry.pacer
}

func (p *keyedRequestPacer) shouldSweep(now time.Time) bool {
	if p.idleTTL <= 0 || p.sweepInterval <= 0 {
		return false
	}
	return p.lastSweep.IsZero() || now.Sub(p.lastSweep) >= p.sweepInterval
}

func (p *keyedRequestPacer) sweep(now time.Time) {
	p.lastSweep = now
	if p.idleTTL <= 0 {
		return
	}
	for key, entry := range p.entries {
		if entry == nil || now.Sub(entry.lastUsed) >= p.idleTTL {
			delete(p.entries, key)
		}
	}
}

func (a *Adapter) ensureCreatePacer() *requestPacer {
	a.paceMu.Lock()
	defer a.paceMu.Unlock()
	if a.createPacer == nil {
		a.createPacer = newRequestPacer(feishuMessageCreateQPS)
	}
	return a.createPacer
}

func (a *Adapter) ensurePatchPacer() *keyedRequestPacer {
	a.paceMu.Lock()
	defer a.paceMu.Unlock()
	if a.patchPacer == nil {
		a.patchPacer = newKeyedRequestPacer(feishuMessagePatchQPS)
	}
	return a.patchPacer
}

func (a *Adapter) createMessage(ctx context.Context, req *larkim.CreateMessageReq) (*larkim.CreateMessageResp, error) {
	if delay, err := a.ensureCreatePacer().Wait(ctx); err != nil {
		return nil, err
	} else if delay > 0 {
		slog.Debug("feishu outbound paced", "op", "send", "delay_ms", delay.Milliseconds())
	}
	resp, err := withFeishuTenantTokenRefreshRetry(ctx, a, "im.message.create", func(client *lark.Client) (*larkim.CreateMessageResp, error) {
		return client.Im.Message.Create(ctx, req)
	})
	if err != nil {
		a.noteOutboundTransportFailure(err)
	}
	return resp, err
}

func (a *Adapter) patchMessage(ctx context.Context, messageID string, req *larkim.PatchMessageReq) (*larkim.PatchMessageResp, error) {
	if delay, err := a.ensurePatchPacer().Wait(ctx, messageID); err != nil {
		return nil, err
	} else if delay > 0 {
		slog.Debug("feishu outbound paced", "op", "patch", "message_id", strings.TrimSpace(messageID), "delay_ms", delay.Milliseconds())
	}
	resp, err := withFeishuTenantTokenRefreshRetry(ctx, a, "im.message.patch", func(client *lark.Client) (*larkim.PatchMessageResp, error) {
		return client.Im.Message.Patch(ctx, req)
	})
	if err != nil {
		a.noteOutboundTransportFailure(err)
	}
	return resp, err
}
