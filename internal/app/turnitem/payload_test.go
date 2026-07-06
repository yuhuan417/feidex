package turnitem

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTurnItemCardPayload(t *testing.T) {
	if got, ok := BuildTurnItemCardPayload("item-nil", nil, ""); ok || got != (CardPayload{}) {
		t.Fatalf("BuildTurnItemCardPayload(nil) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-user", map[string]any{
		"type": "userMessage",
		"text": "hello",
	}, ""); ok || got != (CardPayload{}) {
		t.Fatalf("BuildTurnItemCardPayload(user) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-reasoning-empty", map[string]any{
		"type":    "reasoning",
		"summary": []any{},
	}, ""); ok || got != (CardPayload{}) {
		t.Fatalf("expected empty payload for empty reasoning, got: %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-plan", map[string]any{
		"type": "plan",
		"text": " step 1 ",
	}, ""); !ok || !strings.Contains(got.SummaryText, "计划:\nstep 1") {
		t.Fatalf("BuildTurnItemCardPayload(plan) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-1", map[string]any{
		"type": "agent_message",
		"content": []any{
			map[string]any{"type": "output_text", "text": "final text"},
		},
	}, ""); !ok || got.ItemType != "agent_message" || got.SummaryText != "final text" || !got.IsFinalAnswer {
		t.Fatalf("BuildTurnItemCardPayload(agent message) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-commentary", map[string]any{
		"type":  "agent_message",
		"text":  "working note",
		"phase": "commentary",
	}, ""); !ok || got.IsFinalAnswer || got.SummaryText != "working note" {
		t.Fatalf("BuildTurnItemCardPayload(commentary agent message) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-final", map[string]any{
		"type":  "agent_message",
		"text":  "final text",
		"phase": "final_answer",
	}, ""); !ok || !got.IsFinalAnswer || got.SummaryText != "final text" {
		t.Fatalf("BuildTurnItemCardPayload(final answer) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-review", map[string]any{
		"type":   "exitedReviewMode",
		"review": "review text",
	}, ""); !ok || got.ItemType != "agent_message" || got.ProtocolItemType != "exited_review_mode" || !got.IsFinalAnswer || got.SummaryText != "review text" {
		t.Fatalf("BuildTurnItemCardPayload(exitedReviewMode) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-2", map[string]any{
		"type":              "command_execution",
		"status":            "completed",
		"exit_code":         float64(0),
		"command":           "pwd",
		"aggregated_output": "/tmp/work\n",
	}, ""); !ok {
		t.Fatal("expected command execution payload")
	} else {
		want := "````\npwd\n````\nstatus=completed exit_code=0"
		if normalizeMarkdown(got.SummaryText) != want {
			t.Fatalf("unexpected summary text:\nwant: %q\ngot:  %q", want, got.SummaryText)
		}
		if normalizeMarkdown(got.DetailText) != "输出:\n````\n/tmp/work\n````" {
			t.Fatalf("unexpected command detail: %q", got.DetailText)
		}
	}

	if got, ok := BuildTurnItemCardPayload("item-command", map[string]any{
		"type":        "commandExecution",
		"commandLine": "pwd",
		"output":      "/repo\n",
		"state":       "done",
		"exitCode":    JSONNumber("2"),
	}, ""); !ok || !strings.Contains(normalizeMarkdown(got.SummaryText), "status=done exit_code=2") || !strings.Contains(normalizeMarkdown(got.DetailText), "/repo") {
		t.Fatalf("BuildTurnItemCardPayload(commandLine) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-tool", map[string]any{
		"type":   "mcp_tool_call",
		"server": "github",
		"tool":   "search_repos",
		"status": "completed",
		"input": map[string]any{
			"query": "feidex",
			"limit": float64(5),
		},
	}, ""); !ok {
		t.Fatal("expected tool call payload")
	} else {
		if !strings.Contains(normalizeMarkdown(got.SummaryText), "````\ngithub/search_repos\n````") {
			t.Fatalf("expected tool summary code block, got: %q", got.SummaryText)
		}
		if !strings.Contains(got.SummaryText, "- query: `feidex`") || !strings.Contains(got.SummaryText, "- limit: `5`") {
			t.Fatalf("expected tool input summary, got: %q", got.SummaryText)
		}
		if !strings.Contains(normalizeMarkdown(got.DetailText), "```") {
			t.Fatalf("expected tool detail code block, got: %q", got.DetailText)
		}
	}

	if got, ok := BuildTurnItemCardPayload("item-file", map[string]any{
		"type":   "file_change",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": "internal/app/turn_stream.go",
				"kind": "modified",
				"diff": "@@ -1 +1 @@\n-old\n+new",
			},
		},
	}, ""); !ok {
		t.Fatal("expected file change payload")
	} else {
		if !strings.Contains(normalizeMarkdown(got.SummaryText), "````\nchanged=1\nstatus=completed\ninternal/app/turn_stream.go (modified)\n````") {
			t.Fatalf("expected file change summary code block, got: %q", got.SummaryText)
		}
		if !strings.Contains(normalizeMarkdown(got.DetailText), "````\ninternal/app/turn_stream.go (modified)\n````") {
			t.Fatalf("expected file change header code block in detail, got: %q", got.DetailText)
		}
		if !strings.Contains(normalizeMarkdown(got.DetailText), "````diff\n@@ -1 +1 @@\n-old\n+new\n````") {
			t.Fatalf("expected file change diff code block, got: %q", got.DetailText)
		}
	}

	workspace := t.TempDir()
	if got, ok := BuildTurnItemCardPayload("item-file", map[string]any{
		"type":   "file_change",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": filepath.Join(workspace, "internal", "app", "turn_stream.go"),
				"kind": "modified",
			},
		},
	}, workspace); !ok || !strings.Contains(normalizeMarkdown(got.SummaryText), "internal/app/turn_stream.go (modified)") || !strings.Contains(normalizeMarkdown(got.DetailText), "internal/app/turn_stream.go (modified)") {
		t.Fatalf("BuildTurnItemCardPayload(file relative path) = %#v / %v", got, ok)
	}

	if got, ok := BuildTurnItemCardPayload("item-file-empty", map[string]any{
		"type":   "file_change",
		"status": "completed",
	}, ""); !ok || !strings.Contains(got.SummaryText, "文件改动:") || !strings.Contains(got.DetailText, `"type": "file_change"`) {
		t.Fatalf("BuildTurnItemCardPayload(file empty) = %#v / %v", got, ok)
	}
}

func TestTurnItemFormattingHelpers(t *testing.T) {
	if got := BuildLabeledTurnEventText("计划", "step"); got != "计划:\nstep" {
		t.Fatalf("BuildLabeledTurnEventText() = %q", got)
	}
	if got := BuildLabeledTurnEventText("", " body "); got != "body" {
		t.Fatalf("BuildLabeledTurnEventText(empty label) = %q", got)
	}
	if got := BuildLabeledTurnEventText("计划", ""); got != "计划" {
		t.Fatalf("BuildLabeledTurnEventText(empty body) = %q", got)
	}
	if got := SummarizeCommandExecution("pwd", "/tmp", "completed", OptionalIntPointer(0, true)); !strings.Contains(got, "status=completed") {
		t.Fatalf("SummarizeCommandExecution() = %q", got)
	}
	if got := normalizeMarkdown(FormatTurnCommandOutput(" /tmp ")); got != "输出:\n````\n/tmp\n````" {
		t.Fatalf("FormatTurnCommandOutput() = %q", got)
	}
	if summary, detail := SummarizeGenericTurnItem("web_search", map[string]any{"query": "golang"}, ""); !strings.Contains(summary, "golang") || detail == "" {
		t.Fatalf("SummarizeGenericTurnItem(web_search) = %q / %q", summary, detail)
	}
	if got := TurnItemLabel(""); got != "事件" {
		t.Fatalf("TurnItemLabel(empty) = %q", got)
	}
	if got := TurnItemLabel("contextCompaction"); got != "上下文压缩" {
		t.Fatalf("TurnItemLabel(contextCompaction) = %q", got)
	}
	if got := ExtractTurnItemText(map[string]any{"summary": []any{map[string]any{"type": "summary_text", "text": "hello"}}}, "summary", "summary_text"); got != "hello" {
		t.Fatalf("ExtractTurnItemText() = %q", got)
	}
	if got := MarkdownCodeBlock("a```b"); !strings.Contains(got, "a```b") {
		t.Fatalf("MarkdownCodeBlock() = %q, want raw inner triple backticks", got)
	}
	if got := MarkdownCodeBlock("pwd"); !strings.Contains(got, "````\npwd\n````") {
		t.Fatalf("MarkdownCodeBlock() = %q, want whitelist 4-backtick fence", got)
	}
	if got := InlineCodeText(" `a` "); got != "'a'" {
		t.Fatalf("InlineCodeText() = %q", got)
	}
	if got := MarkdownInlineCode(" `a` "); got != "`'a'`" {
		t.Fatalf("MarkdownInlineCode() = %q", got)
	}
	if !IsCodeStyledTurnItem("dynamic_tool_call") || IsCodeStyledTurnItem("reasoning") {
		t.Fatal("IsCodeStyledTurnItem() returned unexpected result")
	}
	if got, ok := IntValue(JSONNumber("7")); !ok || got != 7 {
		t.Fatalf("IntValue(JSONNumber) = %d, %v", got, ok)
	}
	if got, ok := IntValue("bad"); ok || got != 0 {
		t.Fatalf("IntValue(invalid) = %d, %v, want false", got, ok)
	}
	if OptionalIntPointer(1, false) != nil {
		t.Fatal("OptionalIntPointer(false) should return nil")
	}
	if body, meta := SplitCompactMetaLine(MarkdownCodeBlock("pwd") + "\nstatus=completed exit_code=0"); meta != "status=completed · exit_code=0" || !strings.Contains(body, "pwd") {
		t.Fatalf("SplitCompactMetaLine() = %q / %q", body, meta)
	}
	if got := JoinMarkdownSections("a", "", "b"); got != "a\n\nb" {
		t.Fatalf("JoinMarkdownSections() = %q", got)
	}
	if got := StripTurnItemCardHeading("命令执行:\nbody", "命令执行", "command_execution"); got != "body" {
		t.Fatalf("StripTurnItemCardHeading() = %q", got)
	}
	if got := QuietDisplayFileName(filepath.Join("internal", "app", "quiet_mode.go") + ":12"); got != "quiet_mode.go" {
		t.Fatalf("QuietDisplayFileName() = %q", got)
	}
	if got := BuildQuietSearchLine("quiet", "internal/app"); got != "Search `quiet` in `internal/app`" {
		t.Fatalf("BuildQuietSearchLine() = %q", got)
	}
}

func TestTurnItemSummaryBranches(t *testing.T) {
	if summary, detail := SummarizeGenericTurnItem("dynamic_tool_call", map[string]any{
		"tool":   "TodoWrite",
		"status": "completed",
		"input": map[string]any{
			"todos": []any{
				map[string]any{"content": "核对日志", "status": "in_progress"},
				map[string]any{"content": "补卡片摘要", "status": "pending"},
			},
		},
	}, ""); !strings.Contains(normalizeMarkdown(summary), "TodoWrite") ||
		!strings.Contains(summary, "- todos: 2") ||
		!strings.Contains(summary, "[in_progress] 核对日志") ||
		!strings.Contains(summary, "status=completed") ||
		!strings.Contains(detail, `"todos"`) {
		t.Fatalf("SummarizeGenericTurnItem(dynamic) = %q / %q", summary, detail)
	}

	if summary, _ := SummarizeGenericTurnItem("dynamic_tool_call", map[string]any{
		"tool": "TodoWrite",
		"input": map[string]any{
			"todos": []any{
				map[string]any{"content": "待办1", "status": "completed"},
				map[string]any{"content": "待办2", "status": "completed"},
				map[string]any{"content": "待办3", "status": "in_progress"},
				map[string]any{"content": "待办4", "status": "pending"},
				map[string]any{"content": "待办5", "status": "pending"},
			},
		},
	}, ""); !strings.Contains(summary, "[pending] 待办5") || strings.Contains(summary, "还有 1 项待办") {
		t.Fatalf("SummarizeGenericTurnItem(todo expanded) = %q", summary)
	}

	if summary, detail := SummarizeGenericTurnItem("collab_agent_tool_call", map[string]any{
		"tool":   "delegate",
		"status": "queued",
		"input": map[string]any{
			"description": "让子代理排查卡片渲染",
			"task_id":     "task-123",
		},
	}, ""); !strings.Contains(normalizeMarkdown(summary), "delegate") ||
		!strings.Contains(summary, "task-123") ||
		!strings.Contains(summary, "排查卡片渲染") ||
		!strings.Contains(summary, "status=queued") ||
		!strings.Contains(detail, "delegate") {
		t.Fatalf("SummarizeGenericTurnItem(collab) = %q / %q", summary, detail)
	}

	if summary, detail := SummarizeGenericTurnItem("command_execution", map[string]any{
		"output": "ls -la",
	}, ""); !strings.Contains(summary, "命令执行:") || !strings.Contains(normalizeMarkdown(detail), "````\nls -la\n````") {
		t.Fatalf("SummarizeGenericTurnItem(code styled default) = %q / %q", summary, detail)
	}
}

func TestTurnItemCardHelpers(t *testing.T) {
	cases := map[string]string{
		"agentMessage":      "agent_message",
		"commandExecution":  "command_execution",
		"fileChange":        "file_change",
		"reasoning":         "reasoning",
		"command_execution": "command_execution",
	}
	for input, want := range cases {
		if got := NormalizeTurnItemType(input); got != want {
			t.Fatalf("NormalizeTurnItemType(%q) = %q, want %q", input, got, want)
		}
	}

	if got := TurnItemEventKind("dynamicToolCall"); got != "turn_item" {
		t.Fatalf("TurnItemEventKind(dynamicToolCall) = %q", got)
	}
	if got := ReplyTurnItemCardBody(CardPayload{
		ItemType:    "agent_message",
		Title:       "回复",
		SummaryText: "",
		DetailText:  "回复:\nbody",
	}); got != "body" {
		t.Fatalf("ReplyTurnItemCardBody() = %q", got)
	}
	if got := ReplyTurnItemCardTitle(CardPayload{Title: "最终答复", IsFinalAnswer: true}); got != "最终答复" {
		t.Fatalf("ReplyTurnItemCardTitle(final) = %q", got)
	}
	if got := ReplyTurnItemCardTitle(CardPayload{Title: "回复"}); got != "" {
		t.Fatalf("ReplyTurnItemCardTitle(non-final) = %q", got)
	}
	meta, body := CompactTurnItemCardContent(CardPayload{
		ItemType:    "dynamic_tool_call",
		SummaryText: "事件[dynamic_tool_call]:\n" + MarkdownCodeBlock("search") + "\nstatus=completed",
		DetailText:  "detail",
	})
	if meta != "status=completed" || !strings.Contains(normalizeMarkdown(body), "search") {
		t.Fatalf("CompactTurnItemCardContent(dynamic) = %q / %q", meta, body)
	}
	meta, body = CompactTurnItemCardContent(CardPayload{
		ItemType:    "file_change",
		SummaryText: "文件改动:\nsummary",
		DetailText:  "detail",
	})
	if meta != "" || body != "summary" {
		t.Fatalf("CompactTurnItemCardContent(default) = %q / %q", meta, body)
	}
	if title, color := TurnItemCardMeta("agent_message", true); title != "最终答复" || color != "green" {
		t.Fatalf("TurnItemCardMeta(final) = %q, %q", title, color)
	}
	if title, color := TurnItemCardMeta("contextCompaction", false); title != "上下文压缩" || color != "blue" {
		t.Fatalf("TurnItemCardMeta(contextCompaction) = %q, %q", title, color)
	}
}

func normalizeMarkdown(text string) string {
	return strings.TrimSpace(text)
}
