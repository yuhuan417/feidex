package app

import (
	"context"
	"testing"
	"time"

	"feidex/internal/state"
)

func TestStatusRefreshHelpersAndLoop(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.StatusCardID = "status-card-1" }); err != nil {
		t.Fatalf("UpdateSubmission() error = %v", err)
	}

	a.scheduleStatusCardRefresh(sub.ID)
	a.scheduleStatusCardRefresh(sub.ID)
	if ids := a.takePendingStatusCardRefreshes(); len(ids) != 1 || ids[0] != sub.ID {
		t.Fatalf("takePendingStatusCardRefreshes() = %+v, want one id", ids)
	}

	a.scheduleStatusCardRefresh(sub.ID)
	if err := a.refreshStatusCardNow(sub.ID); err != nil {
		t.Fatalf("refreshStatusCardNow() error = %v", err)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want 1", len(ff.patchedCards))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.startStatusRefreshLoop(ctx)
	a.scheduleStatusCardRefresh(sub.ID)
	deadline := time.Now().Add(3 * time.Second)
	for len(ff.patchedCards) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(ff.patchedCards) < 2 {
		t.Fatalf("statusRefreshLoop() did not flush pending refreshes: %+v", ff.patchedCards)
	}
}
