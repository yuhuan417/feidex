package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDeliveryAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	if got := a.workspaceCwd(a.cfg.Workspaces[0].ID); got != a.cfg.Workspaces[0].Cwd {
		t.Fatalf("workspaceCwd(default) = %q", got)
	}
	if got := a.workspaceCwd("missing"); got != "" {
		t.Fatalf("workspaceCwd(missing) = %q", got)
	}

	if got := buildLabeledTurnEventText("", " body "); got != "body" {
		t.Fatalf("buildLabeledTurnEventText(empty label) = %q", got)
	}
	if got := buildLabeledTurnEventText("计划", ""); got != "计划" {
		t.Fatalf("buildLabeledTurnEventText(empty body) = %q", got)
	}

	meta, body := compactTurnItemCardContent(turnItemCardPayload{
		ItemType:    "dynamic_tool_call",
		SummaryText: "事件[dynamic_tool_call]:\n" + markdownCodeBlock("search") + "\nstatus=completed",
		DetailText:  "detail",
	})
	if meta != "status=completed" || !strings.Contains(normalizeCardMarkdown(body), "search") {
		t.Fatalf("compactTurnItemCardContent(dynamic) = %q / %q", meta, body)
	}

	meta, body = compactTurnItemCardContent(turnItemCardPayload{
		ItemType:    "file_change",
		SummaryText: "文件改动:\nsummary",
		DetailText:  "detail",
	})
	if meta != "" || body != "summary" {
		t.Fatalf("compactTurnItemCardContent(default) = %q / %q", meta, body)
	}

	ff.replyCardErr = errors.New("boom")
	ff.replyTextWithIDs = nil
	if ids := a.sendReplyMessages(context.Background(), sub, "", false, "turn_reasoning"); len(ids) != 1 {
		t.Fatalf("sendReplyMessages(fallback ids) = %#v", ids)
	}
	if len(ff.replyTextWithIDs) != 1 || ff.replyTextWithIDs[0] != "任务已结束。" {
		t.Fatalf("sendReplyMessages(fallback texts) = %#v", ff.replyTextWithIDs)
	}

	ff.replyCardErr = nil
	before := len(ff.replyCards)
	if ids := a.sendReplyMessages(context.Background(), sub, "agent body", false, "turn_output"); len(ids) != 1 {
		t.Fatalf("sendReplyMessages(turn_output) = %#v", ids)
	}
	if len(ff.replyCards) != before+1 {
		t.Fatalf("replyCards after turn_output = %d, want %d", len(ff.replyCards), before+1)
	}

	a.cfg.Feishu.Quiet = true
	if ids := a.sendReplyMessages(context.Background(), sub, "hidden", false, "turn_reasoning"); ids != nil {
		t.Fatalf("sendReplyMessages(quiet gated) = %#v, want nil", ids)
	}

	if title, color, replyClass, showHeader := outboundMessageCardMeta("mystery"); title != "状态更新" || color != "blue" || replyClass || !showHeader {
		t.Fatalf("outboundMessageCardMeta(default) = %q, %q, %v, %v", title, color, replyClass, showHeader)
	}
}

func TestFlushTurnStreamAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)

	a.turnStreams["ghost"] = &turnStream{TurnID: "ghost"}
	if result := a.flushTurnStream(context.Background(), "", "ghost"); result != (turnStreamFlushResult{}) {
		t.Fatalf("flushTurnStream(missing submission) = %+v", result)
	}
	if a.turnStreams["ghost"] != nil {
		t.Fatal("flushTurnStream(missing submission) should remove stream")
	}

	a.cfg.Feishu.Quiet = true
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.noteTurnStarted("sess-1", sub)
	stream := a.turnStreams["turn-1"]
	stream.PendingPlan = "- [in_progress] run"
	reasoningKey := quietWorkingEntryKey(quietWorkingReasoningKey, 0)
	stream.QuietWorking = &quietWorkingCard{
		MessageID:  "reuse-plan",
		EntryOrder: []string{reasoningKey},
		Entries:    map[string]string{reasoningKey: "思考中..."},
	}

	if result := a.flushTurnStream(context.Background(), "thread-1", "turn-1"); result != (turnStreamFlushResult{}) {
		t.Fatalf("flushTurnStream(plan reuse) = %+v", result)
	}
	if a.turnStreams["turn-1"] != nil {
		t.Fatal("flushTurnStream(plan reuse) should clear stream")
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards after plan reuse = %d, want 1", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "计划:\n- [in_progress] run") {
		t.Fatalf("patched plan body = %q", body)
	}
}
