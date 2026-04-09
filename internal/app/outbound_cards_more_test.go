package app

import (
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
	actions := action["actions"].([]map[string]any)
	if actions[0]["name"] != "open" {
		t.Fatalf("action name = %#v, want open", actions[0]["name"])
	}
}

func TestRenderMarkdownCardsUsesPlaceholderAndMeta(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{WorkspaceID: "default"}

	reply := a.renderReplyMarkdownCardWithHeaderOptions(nil, sub, "Reply", "green", true, "", nil, false)
	elements := reply["body"].(map[string]any)["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["content"] != " " {
		t.Fatalf("reply placeholder elements = %#v, want single blank markdown", elements)
	}

	compact := a.renderCompactMarkdownCard(sub, "Status", "orange", " status=running ", "hello", []feishu.Button{{Text: "More", Type: "default"}})
	body := compact["body"].(map[string]any)["elements"].([]map[string]any)
	if len(body) != 3 {
		t.Fatalf("compact card elements = %#v, want meta + markdown + action", body)
	}
	if body[0]["tag"] != "div" || body[1]["tag"] != "markdown" || body[2]["tag"] != "action" {
		t.Fatalf("compact card layout = %#v, want div/markdown/action", body)
	}
}
