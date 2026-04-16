package app

import (
	"context"
	"path/filepath"
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

func TestBuildTurnItemCardPayloadReasoningSkipsEmptyContent(t *testing.T) {
	item := map[string]any{
		"type":    "reasoning",
		"summary": []any{},
	}

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-reasoning-empty", item, "")
	if ok || got != (turnItemCardPayload{}) {
		t.Fatalf("expected empty payload for empty reasoning, got: %#v / %v", got, ok)
	}
}

func TestBuildTurnItemCardPayloadAgentMessageUsesCompletedText(t *testing.T) {
	item := map[string]any{
		"type": "agent_message",
		"content": []any{
			map[string]any{"type": "output_text", "text": "final text"},
		},
	}

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-1", item, "")
	if !ok {
		t.Fatal("expected agent message payload")
	}
	if got.ItemType != "agent_message" {
		t.Fatalf("unexpected item type: %q", got.ItemType)
	}
	if got.SummaryText != "final text" {
		t.Fatalf("unexpected summary text: %q", got.SummaryText)
	}
	if got.IsFinalAnswer {
		t.Fatal("phase-less agent message should not be marked final")
	}
}

func TestBuildTurnItemCardPayloadAgentMessageMarksFinalAnswer(t *testing.T) {
	item := map[string]any{
		"type":  "agent_message",
		"text":  "final text",
		"phase": "final_answer",
	}

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-final", item, "")
	if !ok {
		t.Fatal("expected final agent message payload")
	}
	if !got.IsFinalAnswer {
		t.Fatal("expected final_answer phase to be marked")
	}
	if got.SummaryText != "final text" {
		t.Fatalf("unexpected final summary text: %q", got.SummaryText)
	}
}

func TestBuildTurnItemCardPayloadExitedReviewModeUsesUnifiedFinalAnswerPath(t *testing.T) {
	item := map[string]any{
		"type":   "exitedReviewMode",
		"review": "review text",
	}

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-review", item, "")
	if !ok {
		t.Fatal("expected exitedReviewMode payload")
	}
	if got.ItemType != "agent_message" {
		t.Fatalf("unexpected unified item type: %q", got.ItemType)
	}
	if got.ProtocolItemType != "exited_review_mode" {
		t.Fatalf("unexpected protocol item type: %q", got.ProtocolItemType)
	}
	if !got.IsFinalAnswer {
		t.Fatal("expected exitedReviewMode to be treated as final answer")
	}
	if got.SummaryText != "review text" {
		t.Fatalf("unexpected review summary text: %q", got.SummaryText)
	}
}

func TestBuildTurnItemCardPayloadCommandExecutionBuildsSummaryAndDetail(t *testing.T) {
	item := map[string]any{
		"type":              "command_execution",
		"status":            "completed",
		"exit_code":         float64(0),
		"command":           "pwd",
		"aggregated_output": "/tmp/work\n",
	}

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-2", item, "")
	if !ok {
		t.Fatal("expected command execution payload")
	}
	if got.ItemType != "command_execution" {
		t.Fatalf("unexpected item type: %q", got.ItemType)
	}
	want := "````\npwd\n````\nstatus=completed exit_code=0"
	if rendered := normalizeCardMarkdown(got.SummaryText); rendered != want {
		t.Fatalf("unexpected summary text:\nwant: %q\ngot:  %q", want, got.SummaryText)
	}
	if got.DetailText == "" {
		t.Fatal("expected detail text for command execution payload")
	}
	if rendered := normalizeCardMarkdown(got.DetailText); rendered != "输出:\n````\n/tmp/work\n````" {
		t.Fatalf("unexpected command detail: %q", got.DetailText)
	}
	if !strings.Contains(normalizeCardMarkdown(got.DetailText), "````\n/tmp/work\n````") {
		t.Fatalf("expected command output detail to include code block, got: %q", got.DetailText)
	}
}

func TestBuildTurnItemCardPayloadToolCallUsesCodeBlockDetail(t *testing.T) {
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

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-tool", item, "")
	if !ok {
		t.Fatal("expected tool call payload")
	}
	if !strings.Contains(normalizeCardMarkdown(got.SummaryText), "````\ngithub/search_repos\n````") {
		t.Fatalf("expected tool summary code block, got: %q", got.SummaryText)
	}
	if !strings.Contains(normalizeCardMarkdown(got.DetailText), "```") {
		t.Fatalf("expected tool detail code block, got: %q", got.DetailText)
	}
}

func TestBuildTurnItemCardPayloadFileChangeUsesCodeBlockSummaryAndDiffDetail(t *testing.T) {
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

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-file", item, "")
	if !ok {
		t.Fatal("expected file change payload")
	}
	if !strings.Contains(normalizeCardMarkdown(got.SummaryText), "````\nchanged=1\nstatus=completed\ninternal/app/turn_stream.go (modified)\n````") {
		t.Fatalf("expected file change summary code block, got: %q", got.SummaryText)
	}
	if !strings.Contains(normalizeCardMarkdown(got.DetailText), "````\ninternal/app/turn_stream.go (modified)\n````") {
		t.Fatalf("expected file change header code block in detail, got: %q", got.DetailText)
	}
	if !strings.Contains(normalizeCardMarkdown(got.DetailText), "````diff\n@@ -1 +1 @@\n-old\n+new\n````") {
		t.Fatalf("expected file change diff code block, got: %q", got.DetailText)
	}
	if strings.Contains(got.DetailText, "changed=1") {
		t.Fatalf("expected file change detail to avoid repeating summary block, got: %q", got.DetailText)
	}
}

func TestBuildTurnItemCardPayloadFileChangeUsesWorkspaceRelativePath(t *testing.T) {
	workspace := t.TempDir()
	item := map[string]any{
		"type":   "file_change",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": filepath.Join(workspace, "internal", "app", "turn_stream.go"),
				"kind": "modified",
			},
		},
	}

	got, ok := buildTurnItemCardPayloadWithWorkspace("item-file", item, workspace)
	if !ok {
		t.Fatal("expected file change payload")
	}
	if !strings.Contains(normalizeCardMarkdown(got.SummaryText), "internal/app/turn_stream.go (modified)") {
		t.Fatalf("expected workspace-relative path in summary, got: %q", got.SummaryText)
	}
	if !strings.Contains(normalizeCardMarkdown(got.DetailText), "internal/app/turn_stream.go (modified)") {
		t.Fatalf("expected workspace-relative path in detail, got: %q", got.DetailText)
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
		ItemType:    "command_execution",
		Title:       "命令执行",
		Color:       "blue",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd") + "\nstatus=completed",
		DetailText:  "命令执行:\n命令:\n" + markdownCodeBlock("$ pwd") + "\n输出:\n" + markdownCodeBlock("/tmp/work"),
	}

	card := a.renderTurnItemCard(context.Background(), sub, payload, false)
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

	card := a.renderTurnItemCard(context.Background(), sub, payload, false)
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

	card := a.renderTurnItemCard(context.Background(), sub, payload, false)
	header, ok := card["header"].(map[string]any)
	if !ok {
		t.Fatalf("expected final answer header, got: %#v", card["header"])
	}
	title, _ := header["title"].(map[string]any)
	if got, _ := title["content"].(string); got != "最终答复" {
		t.Fatalf("unexpected final answer title: %q", got)
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

	card := a.renderTurnItemCard(context.Background(), sub, payload, false)
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
