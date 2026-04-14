package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func markdownTestTable(name string) string {
	return strings.Join([]string{
		"### " + name,
		"",
		"| col1 | col2 |",
		"| --- | --- |",
		"| " + name + " | value |",
	}, "\n")
}

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

	parts := splitMarkdownByTableLimit(text, 2)
	if len(parts) != 2 {
		t.Fatalf("splitMarkdownByTableLimit() parts = %d, want 2", len(parts))
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

	parts := splitMarkdownByTableLimit(text, 1)
	if len(parts) != 1 {
		t.Fatalf("splitMarkdownByTableLimit() parts = %d, want 1", len(parts))
	}
	if tables := countTablesInMarkdown(parts[0]); tables != 1 {
		t.Fatalf("table count = %d, want 1 real table", tables)
	}
	if !strings.Contains(parts[0], "````md") || !strings.Contains(parts[0], "````") {
		t.Fatalf("four-backtick fences should be preserved, got: %q", parts[0])
	}
}

func TestCountCardComponentNodesCountsTaggedNodes(t *testing.T) {
	card := newMarkdownBodyCard("Title", "blue")
	for i := 0; i < 198; i++ {
		appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "item"})
	}
	if got := countCardComponentNodes(card); got != 199 {
		t.Fatalf("countCardComponentNodes() = %d, want 199", got)
	}
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "overflow"})
	if got := countCardComponentNodes(card); got != 200 {
		t.Fatalf("countCardComponentNodes() after append = %d, want 200", got)
	}
}

func TestSendFinalMessagesWithFooterSplitsReplyCardsByTableLimit(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2"}

	text := strings.Join([]string{
		"final answer",
		"",
		markdownTestTable("t1"),
		"",
		markdownTestTable("t2"),
		"",
		markdownTestTable("t3"),
		"",
		markdownTestTable("t4"),
		"",
		markdownTestTable("t5"),
		"",
		markdownTestTable("t6"),
	}, "\n")

	got := a.sendFinalMessagesWithFooter(context.Background(), sub, text, []string{"footer line"}, false)
	if len(got) != 2 || got[0] != "card-1" || got[1] != "card-2" {
		t.Fatalf("sendFinalMessagesWithFooter() ids = %#v, want two split card ids", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count = %d, want 2", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); got != "最终答复 1/2" {
		t.Fatalf("first split title = %q, want 最终答复 1/2", got)
	}
	if got := cardHeaderTitle(t, ff.replyCards[1]); got != "最终答复 2/2" {
		t.Fatalf("second split title = %q, want 最终答复 2/2", got)
	}
	if tables := countTablesInMarkdown(cardMarkdownContent(t, ff.replyCards[0])); tables != 5 {
		t.Fatalf("first reply card tables = %d, want 5", tables)
	}
	if tables := countTablesInMarkdown(cardMarkdownContent(t, ff.replyCards[1])); tables != 1 {
		t.Fatalf("second reply card tables = %d, want 1", tables)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); strings.Contains(body, "footer line") {
		t.Fatalf("first split card should not include footer: %q", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "footer line") {
		t.Fatalf("last split card missing footer: %q", body)
	}
	updated := a.store.GetSubmission(sub.ID)
	if updated == nil || len(updated.FinalMessageIDs) != 2 {
		t.Fatalf("updated submission = %+v, want 2 final message ids", updated)
	}
}

func TestSendTurnSnapshotCardSplitsReplyTables(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2"}

	snapshot := turnItemSnapshot{
		ItemID:   "item-1",
		ItemType: "agent_message",
		SendText: strings.Join([]string{
			"reply",
			"",
			markdownTestTable("t1"),
			"",
			markdownTestTable("t2"),
			"",
			markdownTestTable("t3"),
			"",
			markdownTestTable("t4"),
			"",
			markdownTestTable("t5"),
			"",
			markdownTestTable("t6"),
		}, "\n"),
		LinkKind: "turn_output",
	}

	got := a.sendTurnSnapshotCard(context.Background(), sub, snapshot)
	if got != "card-1" {
		t.Fatalf("sendTurnSnapshotCard() = %q, want first split card id", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count = %d, want 2", len(ff.replyCards))
	}
	if len(ff.replyTextWithIDs) != 0 {
		t.Fatalf("unexpected text fallback sends: %+v", ff.replyTextWithIDs)
	}
}

func TestSendFinalMessagesWithFooterSplitsLargePayload(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2", "card-3"}

	longParagraph := strings.Repeat("payload-limit-text ", 1400)
	text := strings.Join([]string{
		"intro",
		"",
		longParagraph,
		"",
		longParagraph,
	}, "\n")

	got := a.sendFinalMessagesWithFooter(context.Background(), sub, text, []string{"footer"}, false)
	if len(got) < 2 {
		t.Fatalf("sendFinalMessagesWithFooter() ids = %#v, want payload-driven split", got)
	}
	for i, card := range ff.replyCards {
		payload, err := json.Marshal(card)
		if err != nil {
			t.Fatalf("Marshal(card[%d]) error = %v", i, err)
		}
		if len(payload) > feishuReplyCardMaxPayloadBytes {
			t.Fatalf("card[%d] payload = %d, want <= %d", i, len(payload), feishuReplyCardMaxPayloadBytes)
		}
	}
}

func countTablesInMarkdown(text string) int {
	count := 0
	for _, block := range splitMarkdownBlocks(text) {
		count += block.TableCount
	}
	return count
}

func cardHeaderTitle(t *testing.T, card map[string]any) string {
	t.Helper()
	header, _ := card["header"].(map[string]any)
	if header == nil {
		return ""
	}
	title, _ := header["title"].(map[string]any)
	if title == nil {
		return ""
	}
	content, _ := title["content"].(string)
	return content
}
