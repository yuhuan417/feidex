package app

import "testing"

func TestSnapshotTurnItemAgentMessageUsesCompletedText(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-1",
		ItemType: "agent_message",
		Delta:    "partial text",
	}
	item := map[string]any{
		"type": "agent_message",
		"content": []any{
			map[string]any{"type": "output_text", "text": "final text"},
		},
	}

	got := snapshotTurnItem(buf, item, false)
	if got.ItemType != "agent_message" {
		t.Fatalf("unexpected item type: %q", got.ItemType)
	}
	if got.StoreText != "final text" {
		t.Fatalf("unexpected store text: %q", got.StoreText)
	}
	if got.SendText != "final text" {
		t.Fatalf("unexpected send text: %q", got.SendText)
	}
	if !got.IsOutput {
		t.Fatal("agent message should be treated as output")
	}
}

func TestSnapshotTurnItemCommandExecutionFormatsMessage(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-2",
		ItemType: "command_execution",
		Command:  "pwd",
		Delta:    "/tmp/work\n",
	}
	item := map[string]any{
		"type":       "command_execution",
		"status":     "completed",
		"exit_code":  float64(0),
		"command":    "pwd",
		"aggregated_output": "/tmp/work\n",
	}

	got := snapshotTurnItem(buf, item, false)
	if got.ItemType != "command_execution" {
		t.Fatalf("unexpected item type: %q", got.ItemType)
	}
	if got.StoreText != "/tmp/work" {
		t.Fatalf("unexpected store text: %q", got.StoreText)
	}
	want := "命令执行:\n$ pwd\n/tmp/work\nstatus=completed exit_code=0"
	if got.SendText != want {
		t.Fatalf("unexpected send text:\nwant: %q\ngot:  %q", want, got.SendText)
	}
}

func TestNormalizeTurnItemType(t *testing.T) {
	cases := map[string]string{
		"agentMessage":      "agent_message",
		"commandExecution":  "command_execution",
		"fileChange":        "file_change",
		"reasoning":         "reasoning",
		"command_execution": "command_execution",
	}
	for input, want := range cases {
		if got := normalizeTurnItemType(input); got != want {
			t.Fatalf("normalizeTurnItemType(%q) = %q, want %q", input, got, want)
		}
	}
}
