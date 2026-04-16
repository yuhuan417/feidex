package app

import (
	"context"
	"strings"
	"testing"
)

func TestTurnStreamHelperFunctions(t *testing.T) {
	if got := buildLabeledTurnEventText("计划", "step"); got != "计划:\nstep" {
		t.Fatalf("buildLabeledTurnEventText() = %q", got)
	}
	if got := summarizeCommandExecution("pwd", "/tmp", "completed", optionalIntPointer(0, true)); !strings.Contains(got, "status=completed") {
		t.Fatalf("summarizeCommandExecution() = %q", got)
	}
	if got := normalizeCardMarkdown(formatTurnCommandOutput(" /tmp ")); got != "输出:\n````\n/tmp\n````" {
		t.Fatalf("formatTurnCommandOutput() = %q", got)
	}
	if summary, detail := summarizeGenericTurnItem("web_search", map[string]any{"query": "golang"}); !strings.Contains(summary, "golang") || detail == "" {
		t.Fatalf("summarizeGenericTurnItem(web_search) = %q / %q", summary, detail)
	}
	if got := turnItemLabel(""); got != "事件" {
		t.Fatalf("turnItemLabel(empty) = %q", got)
	}
	if got := turnItemLabel("contextCompaction"); got != "上下文压缩" {
		t.Fatalf("turnItemLabel(contextCompaction) = %q", got)
	}
	if got := extractTurnItemText(map[string]any{"summary": []any{map[string]any{"type": "summary_text", "text": "hello"}}}, "summary", "summary_text"); got != "hello" {
		t.Fatalf("extractTurnItemText() = %q", got)
	}
	if got := markdownCodeBlock("a```b"); !strings.Contains(got, "a```b") {
		t.Fatalf("markdownCodeBlock() = %q, want raw inner triple backticks", got)
	}
	if got := markdownCodeBlock("pwd"); !strings.Contains(got, "````\npwd\n````") {
		t.Fatalf("markdownCodeBlock() = %q, want whitelist 4-backtick fence", got)
	}
	if got := inlineCodeText(" `a` "); got != "'a'" {
		t.Fatalf("inlineCodeText() = %q", got)
	}
	if !isCodeStyledTurnItem("dynamic_tool_call") || isCodeStyledTurnItem("reasoning") {
		t.Fatal("isCodeStyledTurnItem() returned unexpected result")
	}
	if got, ok := intValue(jsonNumber("7")); !ok || got != 7 {
		t.Fatalf("intValue(jsonNumber) = %d, %v", got, ok)
	}
	if got, ok := intValue("bad"); ok || got != 0 {
		t.Fatalf("intValue(invalid) = %d, %v, want false", got, ok)
	}
	if optionalIntPointer(1, false) != nil {
		t.Fatal("optionalIntPointer(false) should return nil")
	}
	if body, meta := splitCompactMetaLine(markdownCodeBlock("pwd") + "\nstatus=completed exit_code=0"); meta != "status=completed · exit_code=0" || !strings.Contains(body, "pwd") {
		t.Fatalf("splitCompactMetaLine() = %q / %q", body, meta)
	}
	if got := joinMarkdownSections("a", "", "b"); got != "a\n\nb" {
		t.Fatalf("joinMarkdownSections() = %q", got)
	}
	if got := stripTurnItemCardHeading("命令执行:\nbody", "命令执行", "command_execution"); got != "body" {
		t.Fatalf("stripTurnItemCardHeading() = %q", got)
	}
}

func TestTurnStreamLifecycleDeliversItemCardsWithoutStoringAccumulation(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.noteTurnStarted("sess-1", sub)
	a.updatePendingPlan("turn-1", "- [in_progress] run")
	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "reason-1", map[string]any{
		"type":    "reasoning",
		"summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}},
	})

	updated := a.store.GetSubmission(sub.ID)
	if updated == nil {
		t.Fatalf("submission after completeTurnItem = %+v", updated)
	}

	result := a.flushTurnStream(context.Background(), "thread-1", "turn-1")
	updated = a.store.GetSubmission(sub.ID)
	if result.SawFinal || updated == nil {
		t.Fatalf("flushTurnStream() = %+v, submission=%+v", result, updated)
	}
	if a.turnStreams["turn-1"] != nil {
		t.Fatal("expected turn stream to be cleared after flush")
	}
	if len(ff.replyCards) < 2 {
		t.Fatalf("reply cards = %d, want plan + item cards", len(ff.replyCards))
	}

	if title, color := turnItemCardMeta("agent_message", true); title != "最终答复" || color != "green" {
		t.Fatalf("turnItemCardMeta(final) = %q, %q", title, color)
	}
	if title, color := turnItemCardMeta("contextCompaction", false); title != "上下文压缩" || color != "blue" {
		t.Fatalf("turnItemCardMeta(contextCompaction) = %q, %q", title, color)
	}
}
