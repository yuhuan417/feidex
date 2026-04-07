package app

import (
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

func TestSnapshotTurnItemReasoningSkipsEmptyContent(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-reasoning-empty",
		ItemType: "reasoning",
	}
	item := map[string]any{
		"type":    "reasoning",
		"summary": []any{},
	}

	got := snapshotTurnItem(buf, item, false)
	if got != (turnItemSnapshot{}) {
		t.Fatalf("expected empty snapshot for empty reasoning, got: %#v", got)
	}
}

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
	if got.IsFinalAnswer {
		t.Fatal("phase-less agent message should not be marked final")
	}
}

func TestSnapshotTurnItemAgentMessageMarksFinalAnswer(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-final",
		ItemType: "agent_message",
	}
	item := map[string]any{
		"type":  "agent_message",
		"text":  "final text",
		"phase": "final_answer",
	}

	got := snapshotTurnItem(buf, item, false)
	if !got.IsFinalAnswer {
		t.Fatal("expected final_answer phase to be marked")
	}
	if got.SendText != "final text" {
		t.Fatalf("unexpected final send text: %q", got.SendText)
	}
}

func TestSnapshotTurnItemCommandExecutionBuildsSummaryAndDetail(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-2",
		ItemType: "command_execution",
		Command:  "pwd",
		Delta:    "/tmp/work\n",
	}
	item := map[string]any{
		"type":              "command_execution",
		"status":            "completed",
		"exit_code":         float64(0),
		"command":           "pwd",
		"aggregated_output": "/tmp/work\n",
	}

	got := snapshotTurnItem(buf, item, false)
	if got.ItemType != "command_execution" {
		t.Fatalf("unexpected item type: %q", got.ItemType)
	}
	if got.StoreText != "/tmp/work" {
		t.Fatalf("unexpected store text: %q", got.StoreText)
	}
	want := "命令执行:\n```\npwd\n```\nstatus=completed exit_code=0"
	if got.SendText != want {
		t.Fatalf("unexpected send text:\nwant: %q\ngot:  %q", want, got.SendText)
	}
	if !got.Expandable {
		t.Fatal("command execution snapshot should be expandable")
	}
	if got.DetailText == "" {
		t.Fatal("expected detail text for command execution snapshot")
	}
	if got.DetailText != "输出:\n```\n/tmp/work\n```" {
		t.Fatalf("unexpected command detail: %q", got.DetailText)
	}
	if !strings.Contains(got.DetailText, "```\n/tmp/work\n```") {
		t.Fatalf("expected command output detail to include code block, got: %q", got.DetailText)
	}
}

func TestSnapshotTurnItemToolCallUsesCodeBlockDetail(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-tool",
		ItemType: "mcp_tool_call",
	}
	item := map[string]any{
		"type":   "mcp_tool_call",
		"server": "github",
		"tool":   "search_repos",
		"status": "completed",
		"input": map[string]any{
			"query": "feidex",
			"limit": float64(5),
		},
	}

	got := snapshotTurnItem(buf, item, false)
	if !strings.Contains(got.SendText, "```\ngithub/search_repos\n```") {
		t.Fatalf("expected tool summary code block, got: %q", got.SendText)
	}
	if !strings.Contains(got.DetailText, "```") {
		t.Fatalf("expected tool detail code block, got: %q", got.DetailText)
	}
}

func TestSnapshotTurnItemFileChangeUsesCodeBlockSummaryAndDiffDetail(t *testing.T) {
	buf := &turnItemBuffer{
		ItemID:   "item-file",
		ItemType: "file_change",
	}
	item := map[string]any{
		"type":   "file_change",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": "internal/app/turn_stream.go",
				"kind": "modified",
				"diff": "@@ -1 +1 @@\n-old\n+new",
			},
		},
	}

	got := snapshotTurnItem(buf, item, false)
	if !strings.Contains(got.SendText, "```\nchanged=1\nstatus=completed\ninternal/app/turn_stream.go (modified)\n```") {
		t.Fatalf("expected file change summary code block, got: %q", got.SendText)
	}
	if !strings.Contains(got.DetailText, "```\ninternal/app/turn_stream.go (modified)\n```") {
		t.Fatalf("expected file change header code block in detail, got: %q", got.DetailText)
	}
	if !strings.Contains(got.DetailText, "```diff\n@@ -1 +1 @@\n-old\n+new\n```") {
		t.Fatalf("expected file change diff code block, got: %q", got.DetailText)
	}
	if strings.Contains(got.DetailText, "changed=1") {
		t.Fatalf("expected file change detail to avoid repeating summary block, got: %q", got.DetailText)
	}
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
		SessionKey:  sub.SessionKey,
		TurnID:      sub.TurnID,
		ItemType:    "command_execution",
		Title:       "命令执行",
		Color:       "blue",
		SummaryText: "命令执行:\n```\npwd\n```\nstatus=completed",
		DetailText:  "命令执行:\n命令:\n```\n$ pwd\n```\n输出:\n```\n/tmp/work\n```",
	}

	card := a.renderTurnItemCard(sub, payload, false, false, "req-1")
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
	if !strings.Contains(body, "```\npwd\n```") {
		t.Fatalf("expected command block in compact body, got: %q", body)
	}
	if !strings.Contains(body, "输出:\n```\n/tmp/work\n```") {
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
		SessionKey:  sub.SessionKey,
		TurnID:      sub.TurnID,
		ItemType:    "agent_message",
		Title:       "回复",
		Color:       "green",
		SummaryText: "回复（未完成）:\nfinal text",
	}

	card := a.renderTurnItemCard(sub, payload, false, false, "")
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
		SessionKey:  sub.SessionKey,
		TurnID:      sub.TurnID,
		ItemType:    "agent_message",
		Title:       "最终答复",
		Color:       "green",
		SummaryText: longText,
	}

	card := a.renderTurnItemCard(sub, payload, false, false, "")
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
		SessionKey:  sub.SessionKey,
		TurnID:      sub.TurnID,
		ItemType:    "file_change",
		Title:       "文件改动",
		Color:       "orange",
		SummaryText: "文件改动:\n```\nchanged=1\nstatus=completed\ninternal/app/turn_stream.go (modified)\n```",
		DetailText:  "```diff\n@@ -1 +1 @@\n-old\n+new\n```",
	}

	card := a.renderTurnItemCard(sub, payload, false, false, "")
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
