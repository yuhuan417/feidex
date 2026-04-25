package app

import (
	"context"
	"strings"
	"testing"

	"feidex/internal/app/turnitem"
)

func TestTurnStreamHelperFunctions(t *testing.T) {
	if got := turnitem.BuildLabeledTurnEventText("计划", "step"); got != "计划:\nstep" {
		t.Fatalf("turnitem.BuildLabeledTurnEventText() = %q", got)
	}
	if got := turnitem.SummarizeCommandExecution("pwd", "/tmp", "completed", turnitem.OptionalIntPointer(0, true)); !strings.Contains(got, "status=completed") {
		t.Fatalf("turnitem.SummarizeCommandExecution() = %q", got)
	}
	if got := normalizeCardMarkdown(turnitem.FormatTurnCommandOutput(" /tmp ")); got != "输出:\n````\n/tmp\n````" {
		t.Fatalf("turnitem.FormatTurnCommandOutput() = %q", got)
	}
	if summary, detail := turnitem.SummarizeGenericTurnItem("web_search", map[string]any{"query": "golang"}, ""); !strings.Contains(summary, "golang") || detail == "" {
		t.Fatalf("turnitem.SummarizeGenericTurnItem(web_search) = %q / %q", summary, detail)
	}
	if got := turnitem.TurnItemLabel(""); got != "事件" {
		t.Fatalf("turnitem.TurnItemLabel(empty) = %q", got)
	}
	if got := turnitem.TurnItemLabel("contextCompaction"); got != "上下文压缩" {
		t.Fatalf("turnitem.TurnItemLabel(contextCompaction) = %q", got)
	}
	if got := turnitem.ExtractTurnItemText(map[string]any{"summary": []any{map[string]any{"type": "summary_text", "text": "hello"}}}, "summary", "summary_text"); got != "hello" {
		t.Fatalf("turnitem.ExtractTurnItemText() = %q", got)
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
	if !turnitem.IsCodeStyledTurnItem("dynamic_tool_call") || turnitem.IsCodeStyledTurnItem("reasoning") {
		t.Fatal("turnitem.IsCodeStyledTurnItem() returned unexpected result")
	}
	if got, ok := turnitem.IntValue(jsonNumber("7")); !ok || got != 7 {
		t.Fatalf("turnitem.IntValue(jsonNumber) = %d, %v", got, ok)
	}
	if got, ok := turnitem.IntValue("bad"); ok || got != 0 {
		t.Fatalf("turnitem.IntValue(invalid) = %d, %v, want false", got, ok)
	}
	if turnitem.OptionalIntPointer(1, false) != nil {
		t.Fatal("turnitem.OptionalIntPointer(false) should return nil")
	}
	if body, meta := turnitem.SplitCompactMetaLine(markdownCodeBlock("pwd") + "\nstatus=completed exit_code=0"); meta != "status=completed · exit_code=0" || !strings.Contains(body, "pwd") {
		t.Fatalf("turnitem.SplitCompactMetaLine() = %q / %q", body, meta)
	}
	if got := turnitem.JoinMarkdownSections("a", "", "b"); got != "a\n\nb" {
		t.Fatalf("turnitem.JoinMarkdownSections() = %q", got)
	}
	if got := turnitem.StripTurnItemCardHeading("命令执行:\nbody", "命令执行", "command_execution"); got != "body" {
		t.Fatalf("turnitem.StripTurnItemCardHeading() = %q", got)
	}
}

func TestTurnStreamLifecycleDeliversItemCardsWithoutStoringAccumulation(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	newTurnStreamService(a).updatePendingPlan("turn-1", "- [in_progress] run")
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "reason-1", map[string]any{
		"type":    "reasoning",
		"summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}},
	})

	updated := a.store.GetSubmission(sub.ID)
	if updated == nil {
		t.Fatalf("submission after completeTurnItem = %+v", updated)
	}

	result := newTurnStreamService(a).flushTurnStream(context.Background(), "thread-1", "turn-1")
	updated = a.store.GetSubmission(sub.ID)
	if result.SawFinal || updated == nil {
		t.Fatalf("flushTurnStream() = %+v, submission=%+v", result, updated)
	}
	if newTurnStreamService(a).turnStreamTracker().Streams["turn-1"] != nil {
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
