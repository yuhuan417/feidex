package app

import (
	"context"
	"encoding/json"
	appdelivery "feidex/internal/app/delivery"
	"strings"
	"testing"

	appcards "feidex/internal/app/cards"
	"feidex/internal/state"
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

	parts := appdelivery.SplitMarkdownByTableLimit(text, 2)
	if len(parts) != 2 {
		t.Fatalf("appdelivery.SplitMarkdownByTableLimit() parts = %d, want 2", len(parts))
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

	parts := appdelivery.SplitMarkdownByTableLimit(text, 1)
	if len(parts) != 1 {
		t.Fatalf("appdelivery.SplitMarkdownByTableLimit() parts = %d, want 1", len(parts))
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
	if got := appdelivery.CountCardComponentNodes(card); got != 199 {
		t.Fatalf("appdelivery.CountCardComponentNodes() = %d, want 199", got)
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "overflow"})
	if got := appdelivery.CountCardComponentNodes(card); got != 200 {
		t.Fatalf("appdelivery.CountCardComponentNodes() after append = %d, want 200", got)
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

	got := sendFinalMessagesWithFooter(a, context.Background(), sub, text, []string{"footer line"}, false)
	if len(got) != 2 || got[0] != "card-1" || got[1] != "card-2" {
		t.Fatalf("sendFinalMessagesWithFooter() ids = %#v, want two split card ids", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count = %d, want 2", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); !strings.Contains(got, "最终答复 1/2") {
		t.Fatalf("first split title = %q, want to contain 最终答复 1/2", got)
	}
	if got := cardHeaderTitle(t, ff.replyCards[1]); !strings.Contains(got, "最终答复 2/2") {
		t.Fatalf("second split title = %q, want to contain 最终答复 2/2", got)
	}
	if tables := countTablesInMarkdown(cardMarkdownContent(t, ff.replyCards[0])); tables != 5 {
		t.Fatalf("first reply card tables = %d, want 5", tables)
	}
	if tables := countTablesInMarkdown(cardMarkdownContent(t, ff.replyCards[1])); tables != 1 {
		t.Fatalf("second reply card tables = %d, want 1", tables)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("first split card missing attention mention: %q", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("second split card should not repeat attention mention: %q", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); strings.Contains(body, "footer line") {
		t.Fatalf("first split card should not include footer: %q", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "footer line") {
		t.Fatalf("last split card missing footer: %q", body)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil {
		t.Fatalf("updated submission = %+v, want retained runtime submission", updated)
	}
}

func TestSendFinalMessagesWithFooterSkipsAttentionWhenQueuePending(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1"}
	if _, err := a.store.UpdateSession("sess-1", func(sess *state.Session) {
		if sess == nil {
			return
		}
		sess.Queue = []string{"sub-queued"}
		sess.Status = "queued"
	}); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	got := sendFinalMessagesWithFooter(a, context.Background(), sub, "final answer", nil, false)
	if len(got) != 1 || got[0] != "card-1" {
		t.Fatalf("sendFinalMessagesWithFooter() ids = %#v, want single final card", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); strings.Contains(body, `<at id=user-1></at>`) {
		t.Fatalf("queued final card should not mention user: %q", body)
	}
}

func TestSendTurnItemCardSplitsReplyTables(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2"}

	payload := turnItemCardPayload{
		ItemID:   "item-1",
		ItemType: "agent_message",
		Title:    "回复",
		Color:    "green",
		SummaryText: strings.Join([]string{
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
	}

	got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, payload, "")
	if got != "card-1" {
		t.Fatalf("sendTurnItemCard() = %q, want first split card id", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count = %d, want 2", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); !strings.Contains(got, "反馈中") {
		t.Fatalf("first reply card title = %q, want to contain 反馈中", got)
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

	got := sendFinalMessagesWithFooter(a, context.Background(), sub, text, []string{"footer"}, false)
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
	for i, card := range ff.replyCards[:len(ff.replyCards)-1] {
		if footer := cardFooterTextForTest(card); strings.TrimSpace(footer) != "" {
			t.Fatalf("split payload card[%d] should not include footer: %q", i, footer)
		}
	}
	if footer := cardFooterTextForTest(ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(footer, "footer") {
		t.Fatalf("last split payload card missing footer: %q", footer)
	}
}

func countTablesInMarkdown(text string) int {
	count := 0
	for _, block := range appdelivery.SplitMarkdownBlocks(text) {
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

func cardFooterTextForTest(card map[string]any) string {
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) == 0 {
		return ""
	}
	last := elements[len(elements)-1]
	text, _ := last["text"].(map[string]any)
	content, _ := text["content"].(string)
	return content
}
