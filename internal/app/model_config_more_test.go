package app

import (
	"testing"

	"feidex/internal/feishu"
)

func TestModelConfigChunkButtonsHelpers(t *testing.T) {
	if got := chunkButtons(nil, 2); got != nil {
		t.Fatalf("chunkButtons(nil) = %#v, want nil", got)
	}

	rows := chunkButtons([]feishu.Button{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	}, 2)
	if len(rows) != 2 || len(rows[0]) != 2 || len(rows[1]) != 1 {
		t.Fatalf("chunkButtons(size=2) = %#v", rows)
	}

	rows = chunkButtons([]feishu.Button{
		{Text: "A"},
		{Text: "B"},
	}, 0)
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("chunkButtons(size<=0) = %#v", rows)
	}

	row := modelCardActionRow([]feishu.Button{{Text: "Button", Value: map[string]any{"action": "pick"}}})
	if tag, _ := row["tag"].(string); tag != "column_set" {
		t.Fatalf("modelCardActionRow() = %#v", row)
	}
}
