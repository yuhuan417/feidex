package app

import (
	"context"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

func cardBodyElements(t *testing.T, card map[string]any) []map[string]any {
	t.Helper()
	body, ok := card["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected card body: %#v", card["body"])
	}
	elements, ok := body["elements"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected card body elements: %#v", body["elements"])
	}
	return elements
}

func TestRenderTurnItemCardUsesCompactMarkdownStyleForCommandExecution(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		TurnID:      "turn-1",
	}
	payload := turnItemCardPayload{
		ItemType:    "command_execution",
		Title:       "命令执行",
		Color:       "blue",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd") + "\nstatus=completed",
		DetailText:  "命令执行:\n命令:\n" + markdownCodeBlock("$ pwd") + "\n输出:\n" + markdownCodeBlock("/tmp/work"),
	}

	card := newOutboundCardService(a).renderTurnItemCard(context.Background(), sub, payload, false)
	elements := cardBodyElements(t, card)
	if len(elements) != 2 {
		t.Fatalf("expected 2 compact elements (meta, markdown), got %d", len(elements))
	}
	if got := elements[0]["tag"]; got != "div" {
		t.Fatalf("unexpected compact meta tag: %#v", got)
	}
	metaText, _ := elements[0]["text"].(map[string]any)
	if got, _ := metaText["content"].(string); got != "status=completed" {
		t.Fatalf("unexpected compact meta text: %q", got)
	}
	body, _ := elements[1]["content"].(string)
	if strings.Contains(body, "命令执行:") {
		t.Fatalf("expected redundant command title to be stripped, got: %q", body)
	}
	if !strings.Contains(body, "````\npwd\n````") {
		t.Fatalf("expected command block in compact body, got: %q", body)
	}
	if !strings.Contains(body, "输出:\n````\n/tmp/work\n````") {
		t.Fatalf("expected output block in compact body, got: %q", body)
	}
}

func TestRenderTurnItemCardUsesSingleMarkdownBodyForReply(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		TurnID:      "turn-1",
	}
	payload := turnItemCardPayload{
		ItemType:    "agent_message",
		Title:       "回复",
		Color:       "green",
		SummaryText: "回复:\nfinal text",
	}

	card := newOutboundCardService(a).renderTurnItemCard(context.Background(), sub, payload, false)
	if _, ok := card["header"]; ok {
		t.Fatalf("expected normal reply card to omit header, got: %#v", card["header"])
	}
	elements := cardBodyElements(t, card)
	if len(elements) != 1 {
		t.Fatalf("expected single markdown element for reply, got %d", len(elements))
	}
	if got := elements[0]["tag"]; got != "markdown" {
		t.Fatalf("unexpected reply tag: %#v", got)
	}
	content, _ := elements[0]["content"].(string)
	if content != "final text" {
		t.Fatalf("expected stripped reply body, got: %q", content)
	}
}

func TestRenderTurnItemCardDoesNotTruncateLongReply(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		TurnID:      "turn-1",
	}
	longText := strings.Repeat("hello ", 200)
	payload := turnItemCardPayload{
		ItemType:      "agent_message",
		Title:         "最终答复",
		Color:         "green",
		SummaryText:   longText,
		IsFinalAnswer: true,
	}

	card := newOutboundCardService(a).renderTurnItemCard(context.Background(), sub, payload, false)
	header, ok := card["header"].(map[string]any)
	if !ok {
		t.Fatalf("expected final answer header, got: %#v", card["header"])
	}
	title, _ := header["title"].(map[string]any)
	if got, _ := title["content"].(string); !strings.Contains(got, "最终答复") {
		t.Fatalf("unexpected final answer title: %q, want to contain 最终答复", got)
	}
	elements := cardBodyElements(t, card)
	if len(elements) != 1 {
		t.Fatalf("unexpected reply elements: %#v", elements)
	}
	content, _ := elements[0]["content"].(string)
	if content != strings.TrimSpace(longText) {
		t.Fatalf("expected full reply without truncation, got len=%d want len=%d", len(content), len(strings.TrimSpace(longText)))
	}
}

func TestRenderTurnItemCardKeepsFileChangeCompact(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		TurnID:      "turn-1",
	}
	payload := turnItemCardPayload{
		ItemType:    "file_change",
		Title:       "文件改动",
		Color:       "orange",
		SummaryText: "文件改动:\n" + markdownCodeBlock("changed=1\nstatus=completed\ninternal/app/turn_stream.go (modified)"),
		DetailText:  markdownCodeBlockWithLang("diff", "@@ -1 +1 @@\n-old\n+new"),
	}

	card := newOutboundCardService(a).renderTurnItemCard(context.Background(), sub, payload, false)
	elements := cardBodyElements(t, card)
	if len(elements) != 1 {
		t.Fatalf("expected compact file-change card, got %d elements", len(elements))
	}
	body, _ := elements[0]["content"].(string)
	if strings.Contains(body, "@@ -1 +1 @@") {
		t.Fatalf("expected file-change card to omit diff in compact mode, got: %q", body)
	}
	if !strings.Contains(body, "internal/app/turn_stream.go (modified)") {
		t.Fatalf("expected file-change summary to remain visible, got: %q", body)
	}
}
