package app

import (
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestMarkdownBodyCardHelpers(t *testing.T) {
	card := newMarkdownBodyCard("Title", "")
	header := card["header"].(map[string]any)
	if header["template"] != "blue" {
		t.Fatalf("header template = %#v, want blue", header["template"])
	}

	appendMarkdownBodyCardElement(map[string]any{}, map[string]any{"tag": "markdown", "content": "body"})
	action := buildMarkdownBodyCardActionElement([]feishu.Button{{Text: "Open", Type: "primary", Name: "open", Value: map[string]any{"id": "1"}}})
	columns := action["columns"].([]map[string]any)
	button := columns[0]["elements"].([]map[string]any)[0]
	if button["name"] != "open" {
		t.Fatalf("button name = %#v, want open", button["name"])
	}
}

func TestRenderMarkdownCardsUsesPlaceholderAndMeta(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{WorkspaceID: "default"}

	reply := cardRendererForApp(a).renderReplyMarkdownCardWithHeaderOptions(nil, sub, "Reply", "green", true, "", nil, false)
	elements := reply["body"].(map[string]any)["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["content"] != " " {
		t.Fatalf("reply placeholder elements = %#v, want single blank markdown", elements)
	}

	compact := cardRendererForApp(a).renderCompactMarkdownCard(sub, "Status", "orange", " status=running ", "hello", []feishu.Button{{Text: "More", Type: "default"}})
	body := compact["body"].(map[string]any)["elements"].([]map[string]any)
	if len(body) != 3 {
		t.Fatalf("compact card elements = %#v, want meta + markdown + button row", body)
	}
	if body[0]["tag"] != "div" || body[1]["tag"] != "markdown" || body[2]["tag"] != "column_set" {
		t.Fatalf("compact card layout = %#v, want div/markdown/column_set", body)
	}
}

func TestPrepareReplyCardMarkdownKeepsPreviewLinksWithLineNumbers(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{WorkspaceID: "default"}

	body := prepareReplyCardMarkdown(a, nil, sub, "[internal/app/outbound_cards.go:117](https://drive.example/file-1)", true)
	if !strings.Contains(body, "[internal/app/outbound_cards.go:117](https://drive.example/file-1)") {
		t.Fatalf("prepareReplyCardMarkdown(preview link) = %q, want preview link preserved", body)
	}
	if strings.Contains(body, "`internal/app/outbound_cards.go:117`") {
		t.Fatalf("prepareReplyCardMarkdown(preview link) = %q, want no duplicate local-path neutralization", body)
	}
}
