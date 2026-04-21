package app

import (
	"strings"
	"testing"
	"time"
)

func TestFinalCardPatchMergesBodyAndFooterUpdates(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.registerFinalCardPatchState("card-1", sub, "最终答复", "green", true, "original body", []string{"elapsed: 1s"})
	if !a.markFinalCardPreviewPending("card-1") {
		t.Fatal("expected preview patch state to exist")
	}
	if !a.updateFinalCardPatchFooterLines("card-1", []string{"context used: 13.0%", "elapsed: 1s"}) {
		t.Fatal("expected footer update to be accepted")
	}
	if !a.updateFinalCardPatchBody("card-1", "rewritten body") {
		t.Fatal("expected body update to be accepted")
	}
	a.markFinalCardPreviewDone("card-1")

	deadline := time.Now().Add(time.Second)
	for len(ff.patchedCards) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected patched final card")
	}

	last := ff.patchedCards[len(ff.patchedCards)-1]
	if body := cardMarkdownContent(t, last); !strings.Contains(body, "rewritten body") {
		t.Fatalf("patched body = %q, want rewritten body", body)
	}
	if footer := cardFooterTextForTest(last); !strings.Contains(footer, "context used: 13.0%") || !strings.Contains(footer, "elapsed: 1s") {
		t.Fatalf("patched footer = %q", footer)
	}
}
