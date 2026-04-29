package usageview

import (
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
)

func TestUsageFormattingHelpers(t *testing.T) {
	if got := FormatUsageRatio(0, 0); got != "-" {
		t.Fatalf("FormatUsageRatio(0,0) = %q, want -", got)
	}
	if got := FormatContextUsedLine(73.25); got != "context used: 73.2%" {
		t.Fatalf("FormatContextUsedLine(73.25) = %q", got)
	}
	if got := FormatContextLeftLine(0, 1000); got != "context left: 100.0%" {
		t.Fatalf("FormatContextLeftLine(zero total) = %q", got)
	}
	if got := FormatContextLeftLine(750, 1000); got != "context left: 25.0%" {
		t.Fatalf("FormatContextLeftLine(750/1000) = %q", got)
	}
	if got := FormatTurnUsageLine(codexrpc.TokenUsageBreakdown{
		InputTokens:           150,
		CachedInputTokens:     90,
		OutputTokens:          50,
		ReasoningOutputTokens: 20,
	}); !strings.Contains(got, "cache 90 (60.0%)") {
		t.Fatalf("FormatTurnUsageLine() = %q, want cache ratio", got)
	}
	if got := FormatTurnElapsedLine(400 * time.Millisecond); got != "elapsed: <1s" {
		t.Fatalf("FormatTurnElapsedLine(<1s) = %q", got)
	}
	if got := FormatTurnElapsedLine(1500 * time.Millisecond); got != "elapsed: 2s" {
		t.Fatalf("FormatTurnElapsedLine(1500ms) = %q", got)
	}
	if got := FormatTurnElapsedLine((2 * time.Hour) + (3 * time.Minute) + (4 * time.Second)); got != "elapsed: 2h3m4s" {
		t.Fatalf("FormatTurnElapsedLine(2h3m4s) = %q", got)
	}
	if got := FormatTurnElapsedLine((26 * time.Hour) + (3 * time.Minute)); got != "elapsed: 1d2h3m" {
		t.Fatalf("FormatTurnElapsedLine(26h3m) = %q", got)
	}
}

func TestRenderThreadUsageCardBody(t *testing.T) {
	body := RenderThreadUsageCardBody("main", "thread-1", codexrpc.ThreadTokenUsage{
		Total: codexrpc.TokenUsageBreakdown{
			TotalTokens:           500,
			InputTokens:           400,
			CachedInputTokens:     200,
			OutputTokens:          100,
			ReasoningOutputTokens: 40,
		},
	}, "context left: 85.0%")

	for _, want := range []string{
		"当前线程: main",
		"thread: `thread-1`",
		"累计 token usage (`total`):",
		"- total: `500`",
		"- cache ratio: `50.0%`",
		"context left: 85.0%",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderThreadUsageCardBody() missing %q:\n%s", want, body)
		}
	}
}
