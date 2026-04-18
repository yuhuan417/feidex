package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

func TestTurnItemPayloadAdditionalBranches(t *testing.T) {
	if got, ok := buildTurnItemCardPayloadWithWorkspace("item-nil", nil, ""); ok || got != (turnItemCardPayload{}) {
		t.Fatalf("buildTurnItemCardPayloadWithWorkspace(nil) = %#v / %v", got, ok)
	}

	if got, ok := buildTurnItemCardPayloadWithWorkspace("item-user", map[string]any{
		"type": "userMessage",
		"text": "hello",
	}, ""); ok || got != (turnItemCardPayload{}) {
		t.Fatalf("buildTurnItemCardPayloadWithWorkspace(user) = %#v / %v", got, ok)
	}

	if got, ok := buildTurnItemCardPayloadWithWorkspace("item-plan", map[string]any{
		"type": "plan",
		"text": " step 1 ",
	}, ""); !ok || !strings.Contains(got.SummaryText, "计划:\nstep 1") {
		t.Fatalf("buildTurnItemCardPayloadWithWorkspace(plan) = %#v / %v", got, ok)
	}

	if got, ok := buildTurnItemCardPayloadWithWorkspace("item-command", map[string]any{
		"type":        "commandExecution",
		"commandLine": "pwd",
		"output":      "/repo\n",
		"state":       "done",
		"exitCode":    jsonNumber("2"),
	}, ""); !ok || !strings.Contains(normalizeCardMarkdown(got.SummaryText), "status=done exit_code=2") || !strings.Contains(normalizeCardMarkdown(got.DetailText), "/repo") {
		t.Fatalf("buildTurnItemCardPayloadWithWorkspace(commandLine) = %#v / %v", got, ok)
	}

	if got, ok := buildTurnItemCardPayloadWithWorkspace("item-file-empty", map[string]any{
		"type":   "file_change",
		"status": "completed",
	}, ""); !ok || !strings.Contains(got.SummaryText, "文件改动:") || !strings.Contains(got.DetailText, `"type": "file_change"`) {
		t.Fatalf("buildTurnItemCardPayloadWithWorkspace(file empty) = %#v / %v", got, ok)
	}

	if summary, detail := summarizeGenericTurnItem("dynamic_tool_call", map[string]any{
		"tool":   "search",
		"status": "completed",
	}); !strings.Contains(normalizeCardMarkdown(summary), "search") || !strings.Contains(summary, "status=completed") || !strings.Contains(detail, "search") {
		t.Fatalf("summarizeGenericTurnItem(dynamic) = %q / %q", summary, detail)
	}

	if summary, detail := summarizeGenericTurnItem("collab_agent_tool_call", map[string]any{
		"tool":   "delegate",
		"status": "queued",
	}); !strings.Contains(normalizeCardMarkdown(summary), "delegate") || !strings.Contains(summary, "status=queued") || !strings.Contains(detail, "delegate") {
		t.Fatalf("summarizeGenericTurnItem(collab) = %q / %q", summary, detail)
	}

	if summary, detail := summarizeGenericTurnItem("command_execution", map[string]any{
		"output": "ls -la",
	}); !strings.Contains(summary, "命令执行:") || !strings.Contains(normalizeCardMarkdown(detail), "````\nls -la\n````") {
		t.Fatalf("summarizeGenericTurnItem(code styled default) = %q / %q", summary, detail)
	}

	if got := turnItemEventKind("dynamicToolCall"); got != "turn_item" {
		t.Fatalf("turnItemEventKind(dynamicToolCall) = %q", got)
	}

	if got := replyTurnItemCardBody(turnItemCardPayload{
		ItemType:    "agent_message",
		Title:       "回复",
		SummaryText: "",
		DetailText:  "回复:\nbody",
	}); got != "body" {
		t.Fatalf("replyTurnItemCardBody() = %q", got)
	}

	if got := replyTurnItemCardTitle(turnItemCardPayload{Title: "最终答复", IsFinalAnswer: true}); got != "最终答复" {
		t.Fatalf("replyTurnItemCardTitle(final) = %q", got)
	}
	if got := replyTurnItemCardTitle(turnItemCardPayload{Title: "回复"}); got != "" {
		t.Fatalf("replyTurnItemCardTitle(non-final) = %q", got)
	}
}

func TestTurnItemDeliveryReuseFallbackAndFinalCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	if got := a.sendTurnEventCardWithReuse(context.Background(), sub, "任务状态", "blue", "body", "turn_terminal", "item-1", "reuse-event"); got != "reuse-event" {
		t.Fatalf("sendTurnEventCardWithReuse(reuse) = %q", got)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards after event reuse = %d, want 1", len(ff.patchedCards))
	}

	if got := a.sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "agent_message",
		SummaryText: "reply body",
	}, "reuse-reply"); got != "reuse-reply" {
		t.Fatalf("sendTurnItemCardWithReuse(reply reuse) = %q", got)
	}
	if len(ff.patchedCards) != 2 {
		t.Fatalf("patchedCards after reply reuse = %d, want 2", len(ff.patchedCards))
	}

	ff.replyCardErr = errors.New("boom")
	ff.replyTextWithIDs = nil
	if got := a.sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd"),
	}, ""); got != "" {
		t.Fatalf("sendTurnItemCardWithReuse(fallback) = %q, want empty return on fallback", got)
	}
	if len(ff.replyTextWithIDs) != 1 || !strings.Contains(ff.replyTextWithIDs[0], "pwd") {
		t.Fatalf("fallback replyTextWithIDs = %#v", ff.replyTextWithIDs)
	}

	ff.replyCardErr = nil
	if got := a.sendEmptyFinalCard(context.Background(), sub, []string{" line-1 ", "", "line-2 "}); got == "" {
		t.Fatal("sendEmptyFinalCard() should return message id")
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "line-1\nline-2") {
		t.Fatalf("sendEmptyFinalCard() body = %q", body)
	}

	before := len(ff.replyCards)
	a.sendSubmissionQueuedNotice(context.Background(), sub)
	if len(ff.replyCards) != before+1 {
		t.Fatalf("sendSubmissionQueuedNotice() replyCards = %d, want %d", len(ff.replyCards), before+1)
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "已加入队列") {
		t.Fatalf("sendSubmissionQueuedNotice() body = %q", body)
	}
}

func TestTurnItemCardAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.cfg.Feishu.Quiet = config.QuietModeProgress
	if got := a.sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd"),
	}, ""); got != "" {
		t.Fatalf("sendTurnItemCardWithReuse(quiet gated) = %q", got)
	}
	if got := a.sendTurnEventCardWithReuse(context.Background(), sub, "思考", "grey", "body", "turn_reasoning", "item-1", ""); got != "" {
		t.Fatalf("sendTurnEventCardWithReuse(quiet gated) = %q", got)
	}

	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	ff.patchCardErr = errors.New("patch boom")
	ff.replyCardID = "fresh-card-id"
	if got := a.sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd"),
	}, "reuse-item"); got != "fresh-card-id" {
		t.Fatalf("sendTurnItemCardWithReuse(reuse fallback) = %q", got)
	}

	ff.patchCardErr = nil
	ff.replyCardID = "reply-item-id"
	if got := a.sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "agent_message",
		SummaryText: "reply body",
	}, ""); got != "reply-item-id" {
		t.Fatalf("sendTurnItemCardWithReuse(reply direct) = %q", got)
	}

	ff.patchCardErr = errors.New("patch boom")
	ff.replyCardID = "fresh-event-id"
	if got := a.sendTurnEventCardWithReuse(context.Background(), sub, "任务状态", "blue", "body", "turn_terminal", "item-2", "reuse-event"); got != "fresh-event-id" {
		t.Fatalf("sendTurnEventCardWithReuse(reuse fallback) = %q", got)
	}

	if got := (&App{}).replyInThreadForSubmission(nil); got {
		t.Fatal("replyInThreadForSubmission(nil) should be false")
	}
}

func TestTurnItemFinalAnswerSchedulesMarkdownPreviewPatch(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardID = "final-card-id"
	ff.rewritePreviewOut = "patched preview body"

	if got := a.sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:      "agent_message",
		SummaryText:   "See [README](README.md)",
		IsFinalAnswer: true,
		Title:         "最终答复",
		Color:         "green",
	}, ""); got != "final-card-id" {
		t.Fatalf("sendTurnItemCardWithReuse(final) = %q, want final-card-id", got)
	}

	deadline := time.Now().Add(1 * time.Second)
	for len(ff.patchedCards) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected final turn item to patch markdown preview asynchronously")
	}
	if body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1]); !strings.Contains(body, "patched preview body") {
		t.Fatalf("patched final turn item body = %q, want rewritten preview content", body)
	}
}

func TestTurnItemFinalAnswerFooterStaysOnLastSplitCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2", "card-3"}
	a.bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	modelContextWindow := int64(1000)
	a.markTurnStartedAt("turn-1", time.Now().Add(-3*time.Second))
	a.recordTurnTokenUsage("thread-1", "turn-1", codexrpc.ThreadTokenUsage{
		Last: codexrpc.TokenUsageBreakdown{
			InputTokens: 150,
		},
		ModelContextWindow: &modelContextWindow,
	})

	longParagraph := strings.Repeat("payload-limit-text ", 1400)
	got := a.sendTurnItemCard(context.Background(), sub, turnItemCardPayload{
		ItemID:        "item-final",
		ItemType:      "agent_message",
		Title:         "最终答复",
		Color:         "green",
		SummaryText:   "intro\n\n" + longParagraph + "\n\n" + longParagraph,
		IsFinalAnswer: true,
	})
	if got != "card-1" {
		t.Fatalf("sendTurnItemCard(final split) = %q, want card-1", got)
	}
	if len(ff.replyCards) < 2 {
		t.Fatalf("reply card count = %d, want payload-driven split", len(ff.replyCards))
	}
	for i, card := range ff.replyCards[:len(ff.replyCards)-1] {
		if footer := cardFooterTextForTest(card); strings.TrimSpace(footer) != "" {
			t.Fatalf("final split card[%d] should not include footer lines: %q", i, footer)
		}
	}
	lastFooter := cardFooterTextForTest(ff.replyCards[len(ff.replyCards)-1])
	if !strings.Contains(lastFooter, "耗时") && !strings.Contains(lastFooter, "context left") {
		t.Fatalf("last final split card missing footer lines: %q", lastFooter)
	}
}
