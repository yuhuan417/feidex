package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"strings"
	"testing"
)

func TestReplyCardSplitHelpersAndExpansion(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	chunk := appdelivery.ReplyCardChunk{
		ShowHeader:  true,
		Body:        strings.Repeat("abcdef", 7000),
		FooterLines: []string{"footer line"},
	}

	expanded := expandReplyCardChunkToFit(a, context.Background(), sub, "最终答复", "green", chunk, false)
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
