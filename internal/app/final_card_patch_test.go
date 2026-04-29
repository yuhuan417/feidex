package app

import (
	"strings"
	"testing"
)

func TestFinalCardPatchMergesBodyAndFooterUpdates(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	svc := newFinalCardPatchService(a)
	svc.registerFinalCardPatchState("card-1", sub, "最终答复", "green", true, "original body", []string{"elapsed: 1s"})
	if !svc.markFinalCardPreviewPending("card-1") {
		t.Fatal("expected preview patch state to exist")
	}
	if !svc.updateFinalCardPatchFooterLines("card-1", []string{"context used: 13.0%", "elapsed: 1s"}) {
		t.Fatal("expected footer update to be accepted")
	}
	if !svc.updateFinalCardPatchBody("card-1", "rewritten body") {
		t.Fatal("expected body update to be accepted")
	}
	svc.markFinalCardPreviewDone("card-1")

	waitForTestCondition(t, "final card patch", func() bool {
		return len(ff.patchedCardsSnapshot()) > 0
	})
	patched := ff.patchedCardsSnapshot()
	if len(patched) == 0 {
		t.Fatal("expected patched final card")
	}

	last := patched[len(patched)-1]
	body := cardMarkdownContent(t, last)
	footer := cardFooterTextForTest(last)
	if !strings.Contains(body, "rewritten body") {
		t.Logf("all %d patched cards:", len(patched))
		for i, p := range patched {
			t.Logf("  [%d] body=%q footer=%q", i, cardMarkdownContent(t, p), cardFooterTextForTest(p))
		}
		t.Fatalf("patched body = %q, want rewritten body (footer=%q)", body, footer)
	}
	if !strings.Contains(footer, "context used: 13.0%") || !strings.Contains(footer, "elapsed: 1s") {
		t.Fatalf("patched footer = %q", footer)
	}
}
