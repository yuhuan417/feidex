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

	action = BuildMarkdownBodyCardActionElement([]feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"id": "back"}},
		{Text: "Next", Type: "default", Value: map[string]any{"id": "next"}},
	})
	columns = action["columns"].([]map[string]any)
	firstText := columns[0]["elements"].([]map[string]any)[0]["text"].(map[string]any)["content"]
	secondText := columns[1]["elements"].([]map[string]any)[0]["text"].(map[string]any)["content"]
	if firstText != "Next" || secondText != "返回上一级" {
		t.Fatalf("BuildMarkdownBodyCardActionElement(back-last) = %#v", action)
	}

	rows := BuildMarkdownBodyCardActionElements([]feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"id": "back"}},
		{Text: "Next", Type: "default", Value: map[string]any{"id": "next"}},
	})
	if len(rows) != 2 {
		t.Fatalf("BuildMarkdownBodyCardActionElements(rows) = %#v", rows)
	}
	firstRowText := rows[0]["columns"].([]map[string]any)[0]["elements"].([]map[string]any)[0]["text"].(map[string]any)["content"]
	secondRowText := rows[1]["columns"].([]map[string]any)[0]["elements"].([]map[string]any)[0]["text"].(map[string]any)["content"]
	if firstRowText != "Next" || secondRowText != "返回上一级" {
		t.Fatalf("BuildMarkdownBodyCardActionElements(back-last) = %#v", rows)
	}
}
