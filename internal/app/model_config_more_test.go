package app

import (
	"testing"

	"feidex/internal/app/modelconfig"
	"feidex/internal/feishu"
)

func TestModelConfigChunkButtonsHelpers(t *testing.T) {
	if got := modelconfig.ChunkButtons(nil, 2); got != nil {
		t.Fatalf("modelconfig.ChunkButtons(nil) = %#v, want nil", got)
	}

	rows := modelconfig.ChunkButtons([]feishu.Button{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	}, 2)
	if len(rows) != 2 || len(rows[0]) != 2 || len(rows[1]) != 1 {
		t.Fatalf("modelconfig.ChunkButtons(size=2) = %#v", rows)
	}

	rows = modelconfig.ChunkButtons([]feishu.Button{
		{Text: "A"},
		{Text: "B"},
	}, 0)
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("modelconfig.ChunkButtons(size<=0) = %#v", rows)
	}

	row := modelCardActionRow([]feishu.Button{{Text: "Button", Value: map[string]any{"action": "pick"}}})
	if tag, _ := row["tag"].(string); tag != "column_set" {
		t.Fatalf("modelCardActionRow() = %#v", row)
	}
}
