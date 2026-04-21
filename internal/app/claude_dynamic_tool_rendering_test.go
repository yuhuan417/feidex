package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
)

func TestBuildTurnItemCardPayloadWithWorkspaceUsesClaudeDynamicToolTemplates(t *testing.T) {
	workspace := t.TempDir()

	readPayload, ok := buildTurnItemCardPayloadWithWorkspace("item-read", map[string]any{
		"type":   "dynamic_tool_call",
		"tool":   "Read",
		"status": "completed",
		"input": map[string]any{
			"file_path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
			"cwd":       filepath.Join(workspace, "internal", "app"),
		},
	}, workspace)
	if !ok {
		t.Fatal("expected categorized dynamic tool payload")
	}
	if readPayload.Title != "代码工具" || readPayload.Color != "orange" {
		t.Fatalf("unexpected read payload meta: %+v", readPayload)
	}
	if !strings.Contains(readPayload.SummaryText, "- 工具: `Read`") {
		t.Fatalf("expected Read tool line, got: %q", readPayload.SummaryText)
	}
	if !strings.Contains(readPayload.SummaryText, "- 读取: `internal/app/quiet_mode.go`") {
		t.Fatalf("expected workspace-relative file line, got: %q", readPayload.SummaryText)
	}
	if !strings.Contains(readPayload.SummaryText, "status=completed") {
		t.Fatalf("expected status line, got: %q", readPayload.SummaryText)
	}

	mcpPayload, ok := buildTurnItemCardPayloadWithWorkspace("item-mcp", map[string]any{
		"type":   "dynamic_tool_call",
		"tool":   "mcp__demo_tools__greet",
		"status": "completed",
		"input": map[string]any{
			"query": "hello world",
		},
	}, workspace)
	if !ok {
		t.Fatal("expected MCP dynamic tool payload")
	}
	if mcpPayload.Title != "MCP 工具" || mcpPayload.Color != "blue" {
		t.Fatalf("unexpected MCP payload meta: %+v", mcpPayload)
	}
	if !strings.Contains(mcpPayload.SummaryText, "- 工具: `demo_tools/greet`") {
		t.Fatalf("expected display MCP tool name, got: %q", mcpPayload.SummaryText)
	}
	if !strings.Contains(mcpPayload.SummaryText, "- query: `hello world`") {
		t.Fatalf("expected summarized MCP input, got: %q", mcpPayload.SummaryText)
	}

	unknownPayload, ok := buildTurnItemCardPayloadWithWorkspace("item-raw", map[string]any{
		"type":   "dynamic_tool_call",
		"tool":   "StrangeTool",
		"status": "completed",
		"input": map[string]any{
			"foo": "bar",
		},
	}, workspace)
	if !ok {
		t.Fatal("expected raw fallback dynamic tool payload")
	}
	if unknownPayload.Title != "Claude 工具" || unknownPayload.Color != "grey" {
		t.Fatalf("unexpected raw fallback meta: %+v", unknownPayload)
	}
	if strings.TrimSpace(unknownPayload.SummaryText) != "" {
		t.Fatalf("expected empty summary for raw fallback, got: %q", unknownPayload.SummaryText)
	}
	if !strings.Contains(unknownPayload.DetailText, `"tool": "StrangeTool"`) || !strings.Contains(unknownPayload.DetailText, `"foo": "bar"`) {
		t.Fatalf("expected raw JSON detail fallback, got: %q", unknownPayload.DetailText)
	}
	meta, body := compactTurnItemCardContent(unknownPayload)
	if meta != "" || !strings.Contains(body, `"tool": "StrangeTool"`) {
		t.Fatalf("compactTurnItemCardContent(raw fallback) = %q / %q", meta, body)
	}
}

func TestBuildQuietWorkingCardLinesSupportsClaudeDynamicTools(t *testing.T) {
	workspace := t.TempDir()

	_, readLines := buildQuietWorkingCardLines("item-read", map[string]any{
		"type": "dynamic_tool_call",
		"tool": "Read",
		"input": map[string]any{
			"file_path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
		},
	}, workspace)
	if len(readLines) != 1 || readLines[0] != "Read `quiet_mode.go`" {
		t.Fatalf("read progress lines = %#v", readLines)
	}

	_, bashLines := buildQuietWorkingCardLines("item-bash", map[string]any{
		"type": "dynamic_tool_call",
		"tool": "Bash",
		"input": map[string]any{
			"command": "go test ./internal/app",
			"cwd":     filepath.Join(workspace, "internal", "app"),
		},
	}, workspace)
	joinedBash := strings.Join(bashLines, "\n")
	if !strings.Contains(joinedBash, "Run `go test ./internal/app`") || !strings.Contains(joinedBash, "In `internal/app`") {
		t.Fatalf("bash progress lines = %q", joinedBash)
	}

	_, todoLines := buildQuietWorkingCardLines("item-todo", map[string]any{
		"type": "dynamic_tool_call",
		"tool": "TodoWrite",
		"input": map[string]any{
			"todos": []any{
				map[string]any{"content": "核对日志", "status": "in_progress"},
				map[string]any{"content": "补卡片", "status": "pending"},
			},
		},
	}, workspace)
	joinedTodo := strings.Join(todoLines, "\n")
	if !strings.Contains(joinedTodo, "Update todo list (2 items)") || !strings.Contains(joinedTodo, "Todo [in_progress] 核对日志") {
		t.Fatalf("todo progress lines = %q", joinedTodo)
	}

	_, taskLines := buildQuietWorkingCardLines("item-task", map[string]any{
		"type": "dynamic_tool_call",
		"tool": "TaskUpdate",
		"input": map[string]any{
			"taskId":     "7",
			"status":     "in_progress",
			"activeForm": "排查飞书卡片渲染",
		},
	}, workspace)
	joinedTask := strings.Join(taskLines, "\n")
	if !strings.Contains(joinedTask, "Update task `7` -> `in_progress`") || !strings.Contains(joinedTask, "Progress `排查飞书卡片渲染`") {
		t.Fatalf("task progress lines = %q", joinedTask)
	}

	_, unknownLines := buildQuietWorkingCardLines("item-unknown", map[string]any{
		"type": "dynamic_tool_call",
		"tool": "StrangeTool",
		"input": map[string]any{
			"foo": "bar",
		},
	}, workspace)
	if unknownLines != nil {
		t.Fatalf("unknown dynamic tool should stay verbose-only, got: %#v", unknownLines)
	}
}

func TestCompleteTurnItemProgressModeAggregatesSelectedClaudeDynamicToolsOnly(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeProgress
	workspace := a.cfg.Workspaces[0].Cwd
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.noteTurnStarted("sess-1", sub)
	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "item-read", map[string]any{
		"id":   "item-read",
		"type": "dynamic_tool_call",
		"tool": "Read",
		"input": map[string]any{
			"file_path": filepath.Join(workspace, "internal", "app", "quiet_mode.go"),
		},
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after dynamic read = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "Read `quiet_mode.go`") {
		t.Fatalf("working card body after dynamic read = %q", body)
	}

	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "item-task", map[string]any{
		"id":   "item-task",
		"type": "dynamic_tool_call",
		"tool": "TaskUpdate",
		"input": map[string]any{
			"taskId": "7",
			"status": "in_progress",
		},
	})
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count after task update = %d, want 1", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "Update task `7` -> `in_progress`") {
		t.Fatalf("working card body after task update = %q", body)
	}

	replyCount := len(ff.replyCards)
	patchCount := len(ff.patchedCards)
	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "item-unknown", map[string]any{
		"id":   "item-unknown",
		"type": "dynamic_tool_call",
		"tool": "StrangeTool",
		"input": map[string]any{
			"foo": "bar",
		},
	})
	if len(ff.replyCards) != replyCount || len(ff.patchedCards) != patchCount {
		t.Fatalf("unknown dynamic tool should not change progress cards, reply=%d patch=%d", len(ff.replyCards), len(ff.patchedCards))
	}
}
