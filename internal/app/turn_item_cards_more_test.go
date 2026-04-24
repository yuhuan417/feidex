package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
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
		"tool":   "TodoWrite",
		"status": "completed",
		"input": map[string]any{
			"todos": []any{
				map[string]any{"content": "核对日志", "status": "in_progress"},
				map[string]any{"content": "补卡片摘要", "status": "pending"},
			},
		},
	}, ""); !strings.Contains(normalizeCardMarkdown(summary), "TodoWrite") ||
		!strings.Contains(summary, "- todos: 2") ||
		!strings.Contains(summary, "[in_progress] 核对日志") ||
		!strings.Contains(summary, "status=completed") ||
		!strings.Contains(detail, `"todos"`) {
		t.Fatalf("summarizeGenericTurnItem(dynamic) = %q / %q", summary, detail)
	}

	if summary, _ := summarizeGenericTurnItem("dynamic_tool_call", map[string]any{
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
		t.Fatalf("summarizeGenericTurnItem(todo expanded) = %q", summary)
	}

	if summary, detail := summarizeGenericTurnItem("collab_agent_tool_call", map[string]any{
		"tool":   "delegate",
		"status": "queued",
		"input": map[string]any{
			"description": "让子代理排查卡片渲染",
			"task_id":     "task-123",
		},
	}, ""); !strings.Contains(normalizeCardMarkdown(summary), "delegate") ||
		!strings.Contains(summary, "task-123") ||
		!strings.Contains(summary, "排查卡片渲染") ||
		!strings.Contains(summary, "status=queued") ||
		!strings.Contains(detail, "delegate") {
		t.Fatalf("summarizeGenericTurnItem(collab) = %q / %q", summary, detail)
	}

	if summary, detail := summarizeGenericTurnItem("command_execution", map[string]any{
		"output": "ls -la",
	}, ""); !strings.Contains(summary, "命令执行:") || !strings.Contains(normalizeCardMarkdown(detail), "````\nls -la\n````") {
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

	if got := newOutboundCardService(a).sendTurnEventCardWithReuse(context.Background(), sub, "任务状态", "blue", "body", "turn_terminal", "item-1", "reuse-event"); got != "reuse-event" {
		t.Fatalf("sendTurnEventCardWithReuse(reuse) = %q", got)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards after event reuse = %d, want 1", len(ff.patchedCards))
	}

	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
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
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd"),
	}, ""); got != "" {
		t.Fatalf("sendTurnItemCardWithReuse(fallback) = %q, want empty return on fallback", got)
	}
	if len(ff.replyTextWithIDs) != 1 || !strings.Contains(ff.replyTextWithIDs[0], "pwd") {
		t.Fatalf("fallback replyTextWithIDs = %#v", ff.replyTextWithIDs)
	}

	ff.replyCardErr = nil
	if got := sendEmptyFinalCard(a, context.Background(), sub, []string{" line-1 ", "", "line-2 "}); got == "" {
		t.Fatal("sendEmptyFinalCard() should return message id")
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "line-1\nline-2") {
		t.Fatalf("sendEmptyFinalCard() body = %q", body)
	}

	before := len(ff.replyCards)
	sendSubmissionQueuedNotice(a, context.Background(), sub)
	if len(ff.replyCards) != before+1 {
		t.Fatalf("sendSubmissionQueuedNotice() replyCards = %d, want %d", len(ff.replyCards), before+1)
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "已加入队列") {
		t.Fatalf("sendSubmissionQueuedNotice() body = %q", body)
	}

	if err := a.store.UpdateSubmission(sub.ID, func(current *state.Submission) {
		current.WaitedInQueue = true
		current.StartNoticeSent = false
	}); err != nil {
		t.Fatalf("UpdateSubmission(waited in queue) error = %v", err)
	}
	sub = a.store.GetSubmission(sub.ID)
	if sub == nil {
		t.Fatal("submission missing after queue flag update")
	}
	before = len(ff.replyCards)
	newTurnStreamService(a).noteTurnStarted("sess-1", sub)
	if len(ff.replyCards) != before+1 {
		t.Fatalf("noteTurnStarted(waited in queue) replyCards = %d, want %d", len(ff.replyCards), before+1)
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "已轮到这条消息") {
		t.Fatalf("noteTurnStarted(waited in queue) body = %q", body)
	}
	updatedSub := a.store.GetSubmission(sub.ID)
	if updatedSub == nil || !updatedSub.StartNoticeSent {
		t.Fatalf("submission after started notice = %+v, want StartNoticeSent", updatedSub)
	}
	newTurnStreamService(a).noteTurnStarted("sess-1", updatedSub)
	if len(ff.replyCards) != before+1 {
		t.Fatalf("noteTurnStarted() should not duplicate started notice, replyCards = %d, want %d", len(ff.replyCards), before+1)
	}
}

func TestTurnItemCardAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.cfg.Feishu.Quiet = config.QuietModeProgress
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd"),
	}, ""); got != "" {
		t.Fatalf("sendTurnItemCardWithReuse(quiet gated) = %q", got)
	}
	if got := newOutboundCardService(a).sendTurnEventCardWithReuse(context.Background(), sub, "思考", "grey", "body", "turn_reasoning", "item-1", ""); got != "" {
		t.Fatalf("sendTurnEventCardWithReuse(quiet gated) = %q", got)
	}

	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	ff.patchCardErr = errors.New("patch boom")
	ff.replyCardID = "fresh-card-id"
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		SummaryText: "命令执行:\n" + markdownCodeBlock("pwd"),
	}, "reuse-item"); got != "fresh-card-id" {
		t.Fatalf("sendTurnItemCardWithReuse(reuse fallback) = %q", got)
	}

	ff.patchCardErr = nil
	ff.replyCardID = "reply-item-id"
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:    "agent_message",
		SummaryText: "reply body",
	}, ""); got != "reply-item-id" {
		t.Fatalf("sendTurnItemCardWithReuse(reply direct) = %q", got)
	}

	ff.patchCardErr = errors.New("patch boom")
	ff.replyCardID = "fresh-event-id"
	if got := newOutboundCardService(a).sendTurnEventCardWithReuse(context.Background(), sub, "任务状态", "blue", "body", "turn_terminal", "item-2", "reuse-event"); got != "fresh-event-id" {
		t.Fatalf("sendTurnEventCardWithReuse(reuse fallback) = %q", got)
	}

	if got := replyInThreadForSubmission(&App{}, nil); got {
		t.Fatal("replyInThreadForSubmission(nil) should be false")
	}
}

func TestTurnItemFinalAnswerSchedulesLocalFileLinkPatch(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardID = "final-card-id"
	ff.rewriteLocalFileLinksOut = "patched preview body"

	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemType:      "agent_message",
		SummaryText:   "See [README](README.md)",
		IsFinalAnswer: true,
		Title:         "最终答复",
		Color:         "green",
	}, ""); got != "final-card-id" {
		t.Fatalf("sendTurnItemCardWithReuse(final) = %q, want final-card-id", got)
	}

	waitForTestCondition(t, "local file link patch", func() bool {
		return len(ff.patchedCardsSnapshot()) > 0
	})
	patched := ff.patchedCardsSnapshot()
	if len(patched) == 0 {
		t.Fatal("expected final turn item to patch local file links asynchronously")
	}
	if body := cardMarkdownContent(t, patched[len(patched)-1]); !strings.Contains(body, "patched preview body") {
		t.Fatalf("patched final turn item body = %q, want rewritten preview content", body)
	}
}

func TestTurnItemFinalAnswerFooterStaysOnLastSplitCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardIDs = []string{"card-1", "card-2", "card-3"}
	newRuntimeStateService(a).bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	modelContextWindow := int64(1000)
	newRuntimeStateService(a).markTurnStartedAt("turn-1", time.Now().Add(-3*time.Second))
	newRuntimeStateService(a).recordTurnTokenUsage("thread-1", "turn-1", codexrpc.ThreadTokenUsage{
		Last: codexrpc.TokenUsageBreakdown{
			InputTokens: 150,
		},
		ModelContextWindow: &modelContextWindow,
	})

	longParagraph := strings.Repeat("payload-limit-text ", 1400)
	got := newOutboundCardService(a).sendTurnItemCard(context.Background(), sub, turnItemCardPayload{
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
