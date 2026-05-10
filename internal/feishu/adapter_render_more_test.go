package feishu

import (
	"strings"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestPostMessageAndCardRenderHelpers(t *testing.T) {
	raw := `{
		"title":"Title",
		"content":[[
			{"tag":"text","text":"hello "},
			{"tag":"a","href":"https://example.test"},
			{"tag":"at","user_name":"alice"},
			{"tag":"img","image_key":"img-1"},
			{"tag":"unknown","text":" tail"}
		]]
	}`
	text, attachments, ok := extractPostMessage(strPtr(raw))
	if !ok || !strings.Contains(text, "Title") || !strings.Contains(text, "hello") || !strings.Contains(text, "https://example.test") || !strings.Contains(text, "@alice") || !strings.Contains(text, "tail") {
		t.Fatalf("extractPostMessage(direct) = %q %+v %v", text, attachments, ok)
	}
	if len(attachments) != 1 || attachments[0].ResourceKey != "img-1" {
		t.Fatalf("extractPostMessage(direct attachments) = %+v", attachments)
	}

	localized := `{"zh_cn":{"content":[[{"tag":"text","text":"localized"}]]}}`
	if text, attachments, ok := extractPostMessage(strPtr(localized)); !ok || text != "localized" || len(attachments) != 0 {
		t.Fatalf("extractPostMessage(localized) = %q %+v %v", text, attachments, ok)
	}

	if _, _, ok := extractPostMessage(strPtr(`{"zh_cn":{}}`)); ok {
		t.Fatal("extractPostMessage(empty localized) should fail")
	}
	if postMessageBodyNonEmpty(postMessageBody{}) {
		t.Fatal("postMessageBodyNonEmpty(empty) should be false")
	}
	if text, attachments, ok := renderPostMessageBody(postMessageBody{
		Content: [][]postMessageBlock{{{Tag: "img", ImageKey: "img-only"}}},
	}); !ok || text != "" || len(attachments) != 1 {
		t.Fatalf("renderPostMessageBody(images only) = %q %+v %v", text, attachments, ok)
	}

	preview := ""
	buttonCount := 0
	summarizeCardElementForLog(map[string]any{
		"tag":     "markdown",
		"content": " preview text ",
	}, &preview, &buttonCount)
	if preview != "preview text" || buttonCount != 0 {
		t.Fatalf("summarizeCardElementForLog(markdown) = %q %d", preview, buttonCount)
	}

	preview = ""
	summarizeCardElementForLog(map[string]any{
		"tag": "div",
		"text": map[string]any{
			"content": " div preview ",
		},
	}, &preview, &buttonCount)
	if preview != "div preview" {
		t.Fatalf("summarizeCardElementForLog(div) = %q", preview)
	}

	summarizeCardElementForLog(map[string]any{
		"tag":     "action",
		"actions": []map[string]any{{}, {}},
	}, &preview, &buttonCount)
	if buttonCount != 2 {
		t.Fatalf("summarizeCardElementForLog(action) buttons = %d", buttonCount)
	}

	preview = ""
	summarizeCardElementForLog(map[string]any{
		"tag": "column_set",
		"columns": []map[string]any{
			{
				"elements": []map[string]any{
					{"tag": "button"},
					{"tag": "markdown", "content": " child preview "},
				},
			},
		},
	}, &preview, &buttonCount)
	if preview != "child preview" || buttonCount != 3 {
		t.Fatalf("summarizeCardElementForLog(column_set) = %q %d", preview, buttonCount)
	}

	rows := buildV2ButtonRows([]Button{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	}, 2)
	if len(rows) != 2 {
		t.Fatalf("buildV2ButtonRows() = %#v", rows)
	}
	firstColumns, _ := rows[0]["columns"].([]map[string]any)
	secondColumns, _ := rows[1]["columns"].([]map[string]any)
	if len(firstColumns) != 2 || len(secondColumns) != 1 {
		t.Fatalf("buildV2ButtonRows(columns) = %#v / %#v", firstColumns, secondColumns)
	}
	if got := buildV2ButtonRows([]Button{{Text: "A"}, {Text: "B"}}, 0); len(got) != 1 {
		t.Fatalf("buildV2ButtonRows(rowSize<=0) = %#v", got)
	}
	if got := buildV2ButtonRows(nil, 1); got != nil {
		t.Fatalf("buildV2ButtonRows(nil) = %#v, want nil", got)
	}
	rows = buildV2ButtonRows([]Button{
		{Text: "返回上一级"},
		{Text: "A"},
		{Text: "B"},
	}, 1)
	if len(rows) != 3 {
		t.Fatalf("buildV2ButtonRows(back-last rows) = %#v", rows)
	}
	var labels []string
	for _, row := range rows {
		columns, _ := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			t.Fatalf("buildV2ButtonRows(back-last columns) = %#v", row)
		}
		elements, _ := columns[0]["elements"].([]map[string]any)
		if len(elements) == 0 {
			t.Fatalf("buildV2ButtonRows(back-last elements) = %#v", columns[0])
		}
		text, _ := elements[0]["text"].(map[string]any)
		labels = append(labels, text["content"].(string))
	}
	if strings.Join(labels, ",") != "A,B,返回上一级" {
		t.Fatalf("buildV2ButtonRows(back-last labels) = %v", labels)
	}

	if got := messageBodyContent(nil); got != nil {
		t.Fatalf("messageBodyContent(nil) = %#v", got)
	}
	content := "body"
	if got := messageBodyContent(&larkim.MessageBody{Content: &content}); got == nil || *got != "body" {
		t.Fatalf("messageBodyContent() = %#v", got)
	}
}
