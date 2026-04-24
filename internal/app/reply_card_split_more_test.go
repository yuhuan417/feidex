package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"strings"
	"testing"
)

func TestReplyCardSplitHelpersAndExpansion(t *testing.T) {
	if got := splitReplyTextByRunes("abcdef", func(s string) bool { return len([]rune(s)) <= 2 }); len(got) < 3 {
		t.Fatalf("splitReplyTextByRunes() = %#v", got)
	}

	parts := splitReplyTextBlockToFit(strings.Repeat("abcdef", 40), func(s string) bool { return len([]rune(s)) <= 12 })
	if len(parts) < 2 {
		t.Fatalf("splitReplyTextBlockToFit() = %#v", parts)
	}
	for _, part := range parts {
		if len([]rune(part)) > 12 {
			t.Fatalf("splitReplyTextBlockToFit() produced oversize part %q", part)
		}
	}

	if idx := splitIndexNearMiddle("alpha beta gamma", " "); idx <= 0 {
		t.Fatalf("splitIndexNearMiddle() = %d", idx)
	}
	if left, right := splitReplyTextAt("alpha beta", 5, 1); left != "alpha" || right != "beta" {
		t.Fatalf("splitReplyTextAt() = %q / %q", left, right)
	}
	if got := joinReplyChunkBodies("alpha", " beta "); got != "alpha\nbeta" {
		t.Fatalf("joinReplyChunkBodies() = %q", got)
	}

	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	chunk := appdelivery.ReplyCardChunk{
		ShowHeader:  true,
		Body:        strings.Repeat("abcdef", 7000),
		FooterLines: []string{"footer line"},
	}

	expanded := a.expandReplyCardChunkToFit(context.Background(), sub, "最终答复", "green", chunk, false)
	if len(expanded) < 2 {
		t.Fatalf("expandReplyCardChunkToFit() = %#v", expanded)
	}
	if !expanded[0].ShowHeader {
		t.Fatal("first expanded chunk should keep header")
	}
	for i := 1; i < len(expanded); i++ {
		if expanded[i].ShowHeader {
			t.Fatalf("expanded chunk %d unexpectedly kept header: %#v", i, expanded[i])
		}
	}
	if got := expanded[len(expanded)-1].FooterLines; len(got) != 1 || got[0] != "footer line" {
		t.Fatalf("last expanded footer = %#v", got)
	}
}
