package delivery

import (
	"strings"
	"testing"

	appcards "feidex/internal/app/cards"
)

func TestSplitMarkdownByTableLimitSplitsRealTablesOnly(t *testing.T) {
	text := strings.Join([]string{
		"intro",
		"",
		markdownTestTable("t1"),
		"",
		"```md",
		"| fake | table |",
		"| --- | --- |",
		"| inside | code |",
		"```",
		"",
		markdownTestTable("t2"),
		"",
		markdownTestTable("t3"),
	}, "\n")

	parts := SplitMarkdownByTableLimit(text, 2)
	if len(parts) != 2 {
		t.Fatalf("SplitMarkdownByTableLimit() parts = %d, want 2", len(parts))
	}
	if tables := countTablesInMarkdown(parts[0]); tables != 2 {
		t.Fatalf("first part tables = %d, want 2", tables)
	}
	if tables := countTablesInMarkdown(parts[1]); tables != 1 {
		t.Fatalf("second part tables = %d, want 1", tables)
	}
	if !strings.Contains(parts[0], "| fake | table |") {
		t.Fatalf("first part lost fenced code block: %q", parts[0])
	}
}

func TestSplitMarkdownByTableLimitKeepsFourBacktickFences(t *testing.T) {
	text := strings.Join([]string{
		"intro",
		"",
		"````md",
		"| fake | table |",
		"| --- | --- |",
		"| inside | code |",
		"````",
		"",
		markdownTestTable("t1"),
	}, "\n")

	parts := SplitMarkdownByTableLimit(text, 1)
	if len(parts) != 1 {
		t.Fatalf("SplitMarkdownByTableLimit() parts = %d, want 1", len(parts))
	}
	if tables := countTablesInMarkdown(parts[0]); tables != 1 {
		t.Fatalf("table count = %d, want 1 real table", tables)
	}
	if !strings.Contains(parts[0], "````md") || !strings.Contains(parts[0], "````") {
		t.Fatalf("four-backtick fences should be preserved, got: %q", parts[0])
	}
}

func TestCountCardComponentNodesCountsTaggedNodes(t *testing.T) {
	card := appcards.NewMarkdownBodyCard("Title", "blue")
	for i := 0; i < 198; i++ {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "item"})
	}
	if got := CountCardComponentNodes(card); got != 199 {
		t.Fatalf("CountCardComponentNodes() = %d, want 199", got)
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "overflow"})
	if got := CountCardComponentNodes(card); got != 200 {
		t.Fatalf("CountCardComponentNodes() after append = %d, want 200", got)
	}
}

func TestReplyCardSplitHelpers(t *testing.T) {
	if got := SplitReplyTextByRunes("abcdef", func(s string) bool { return len([]rune(s)) <= 2 }); len(got) < 3 {
		t.Fatalf("SplitReplyTextByRunes() = %#v", got)
	}

	parts := SplitReplyTextBlockToFit(strings.Repeat("abcdef", 40), func(s string) bool { return len([]rune(s)) <= 12 })
	if len(parts) < 2 {
		t.Fatalf("SplitReplyTextBlockToFit() = %#v", parts)
	}
	for _, part := range parts {
		if len([]rune(part)) > 12 {
			t.Fatalf("SplitReplyTextBlockToFit() produced oversize part %q", part)
		}
	}

	if idx := SplitIndexNearMiddle("alpha beta gamma", " "); idx <= 0 {
		t.Fatalf("SplitIndexNearMiddle() = %d", idx)
	}
	if left, right := SplitReplyTextAt("alpha beta", 5, 1); left != "alpha" || right != "beta" {
		t.Fatalf("SplitReplyTextAt() = %q / %q", left, right)
	}
	if got := JoinReplyChunkBodies("alpha", " beta "); got != "alpha\nbeta" {
		t.Fatalf("JoinReplyChunkBodies() = %q", got)
	}
}

func markdownTestTable(name string) string {
	return strings.Join([]string{
		"### " + name,
		"",
		"| col1 | col2 |",
		"| --- | --- |",
		"| " + name + " | value |",
	}, "\n")
}

func countTablesInMarkdown(text string) int {
	count := 0
	for _, block := range SplitMarkdownBlocks(text) {
		count += block.TableCount
	}
	return count
}
