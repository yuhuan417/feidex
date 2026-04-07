package app

import (
	"context"
	"sort"
	"time"
)

const statusCardRefreshDelay = 1500 * time.Millisecond

func (a *App) startStatusRefreshLoop(ctx context.Context) {
	a.statusFlushOnce.Do(func() {
		go a.statusRefreshLoop(ctx)
	})
}

func (a *App) scheduleStatusCardRefresh(submissionID string) {
	if submissionID == "" {
		return
	}
	a.statusFlushMu.Lock()
	if a.statusFlushPending == nil {
		a.statusFlushPending = map[string]struct{}{}
	}
	a.statusFlushPending[submissionID] = struct{}{}
	a.statusFlushMu.Unlock()

	select {
	case a.statusFlushCh <- struct{}{}:
	default:
	}
}

func (a *App) refreshStatusCardNow(submissionID string) error {
	if submissionID == "" {
		return nil
	}
	a.statusFlushMu.Lock()
	delete(a.statusFlushPending, submissionID)
	a.statusFlushMu.Unlock()
	return a.refreshStatusCard(submissionID)
}

func (a *App) statusRefreshLoop(ctx context.Context) {
	var (
		timer   *time.Timer
		timerCh <-chan time.Time
	)
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-a.statusFlushCh:
			if timer == nil {
				timer = time.NewTimer(statusCardRefreshDelay)
				timerCh = timer.C
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(statusCardRefreshDelay)
		case <-timerCh:
			for _, submissionID := range a.takePendingStatusCardRefreshes() {
				_ = a.refreshStatusCard(submissionID)
			}
			timer = nil
			timerCh = nil
		}
	}
}

func (a *App) takePendingStatusCardRefreshes() []string {
	a.statusFlushMu.Lock()
	defer a.statusFlushMu.Unlock()
	if len(a.statusFlushPending) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.statusFlushPending))
	for id := range a.statusFlushPending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	a.statusFlushPending = map[string]struct{}{}
	return ids
}
