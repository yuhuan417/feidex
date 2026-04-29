package cards

import (
	"testing"

	"feidex/internal/feishu"
)

func TestMarkdownBodyCardHelpers(t *testing.T) {
	card := NewMarkdownBodyCard("Title", "")
	header := card["header"].(map[string]any)
	if header["template"] != "blue" {
		t.Fatalf("header template = %#v, want blue", header["template"])
	}

	AppendMarkdownBodyCardElement(map[string]any{}, map[string]any{"tag": "markdown", "content": "body"})
	action := BuildMarkdownBodyCardActionElement([]feishu.Button{{Text: "Open", Type: "primary", Name: "open", Value: map[string]any{"id": "1"}}})
	columns := action["columns"].([]map[string]any)
	button := columns[0]["elements"].([]map[string]any)[0]
	if button["name"] != "open" {
		t.Fatalf("button name = %#v, want open", button["name"])
	}
}
