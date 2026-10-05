package feishuapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	applinkutil "feidex/internal/adapter/feishu/linkutil"
	"feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/config"
)

func TestDeliveryAdditionalBranches(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose

	if got := workspaceCwd(a.cfg, a.cfg.Workspaces[0].ID); got != a.cfg.Workspaces[0].Cwd {
		t.Fatalf("workspaceCwd(default) = %q", got)
	}
	if got := workspaceCwd(a.cfg, "missing"); got != "" {
		t.Fatalf("workspaceCwd(missing) = %q", got)
	}

	if got := turnitem.BuildLabeledTurnEventText("", " body "); got != "body" {
		t.Fatalf("turnitem.BuildLabeledTurnEventText(empty label) = %q", got)
	}
	if got := turnitem.BuildLabeledTurnEventText("计划", ""); got != "计划" {
		t.Fatalf("turnitem.BuildLabeledTurnEventText(empty body) = %q", got)
	}

	meta, body := turnitem.CompactTurnItemCardContent(turnitem.CardPayload{
		ItemType:    "dynamic_tool_call",
		SummaryText: "事件[dynamic_tool_call]:\n" + turnitem.MarkdownCodeBlock("search") + "\nstatus=completed",
		DetailText:  "detail",
	})
	if meta != "status=completed" || !strings.Contains(applinkutil.NormalizeCardMarkdown(body), "search") {
		t.Fatalf("turnitem.CompactTurnItemCardContent(dynamic) = %q / %q", meta, body)
	}

	meta, body = turnitem.CompactTurnItemCardContent(turnitem.CardPayload{
		ItemType:    "file_change",
		SummaryText: "文件改动:\nsummary",
		DetailText:  "detail",
	})
	if meta != "" || body != "summary" {
		t.Fatalf("turnitem.CompactTurnItemCardContent(default) = %q / %q", meta, body)
	}

	if title, color, replyClass, showHeader := outboundMessageCardMeta("mystery"); title != "状态更新" || color != "blue" || replyClass || !showHeader {
		t.Fatalf("outboundMessageCardMeta(default) = %q, %q, %v, %v", title, color, replyClass, showHeader)
	}
}

func TestReplyChunkDeliveryEmptyFinalCardFallbacks(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = config.QuietModeVerbose
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	ff.replyCardErr = errors.New("boom")
	delivery := newOutboundCardService(a).replyChunks

	if got := delivery.SendEmptyFinalCardWithReuse(context.Background(), sub, nil, ""); got != "reply-text-id" {
		t.Fatalf("SendEmptyFinalCardWithReuse() = %q, want reply-text-id fallback", got)
	}
	if len(ff.replyTextWithIDs) != 1 || !strings.Contains(ff.replyTextWithIDs[0], "任务已结束。") {
		t.Fatalf("replyTextWithIDs = %#v, want terminal fallback text", ff.replyTextWithIDs)
	}

	ff.replyCardErr = nil
	sub.TriggerMessageID = ""
	if got := delivery.SendEmptyFinalCardWithReuse(context.Background(), sub, []string{"footer"}, ""); got != "" {
		t.Fatalf("SendEmptyFinalCardWithReuse(no trigger) = %q, want empty", got)
	}
	if len(ff.sentTexts) != 1 || !strings.Contains(ff.sentTexts[0], "任务已结束。\nfooter") {
		t.Fatalf("sentTexts = %#v, want chat fallback with footer", ff.sentTexts)
	}
}

func TestFlushTurnStreamAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)

	a.bindings.TurnPresentation.Tracker().Streams["ghost"] = &turnStream{TurnID: "ghost"}
	if result := a.bindings.TurnPresentation.FlushTurnStream(context.Background(), "", "ghost"); result != (turnStreamFlushResult{}) {
		t.Fatalf("flushTurnStream(missing submission) = %+v", result)
	}
	if a.bindings.TurnPresentation.Tracker().Streams["ghost"] != nil {
		t.Fatal("flushTurnStream(missing submission) should remove stream")
	}

	a.cfg.Feishu.Quiet = config.QuietModeProgress
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.bindings.TurnPresentation.NoteTurnStarted("sess-1", sub)
	stream := a.bindings.TurnPresentation.Tracker().Streams["turn-1"]
	stream.PendingPlan = "- [in_progress] run"
	reasoningKey := turn.EntryKey(turn.QuietWorkingReasoningKey, 0)
	stream.QuietWorking = &turn.QuietWorkingCard{
		MessageID:  "reuse-plan",
		EntryOrder: []string{reasoningKey},
		Entries:    map[string]string{reasoningKey: "思考中..."},
	}
	a.cfg.Feishu.Quiet = config.QuietModeNormal

	result := a.bindings.TurnPresentation.FlushTurnStream(context.Background(), "thread-1", "turn-1")
	if result.SawFinal || result.SawPlanItem || result.PlanCompleted || result.PlanMarkdown != "" || result.LastError != "" || result.WorkingMessageID != "" || result.ShouldUsePlanExitPrompt {
		t.Fatalf("flushTurnStream(plan reuse) unexpected flags = %+v", result)
	}
	if result.PlanMessageID != "reuse-plan" {
		t.Fatalf("flushTurnStream(plan reuse) PlanMessageID = %q, want reuse-plan", result.PlanMessageID)
	}
	if a.bindings.TurnPresentation.Tracker().Streams["turn-1"] != nil {
		t.Fatal("flushTurnStream(plan reuse) should clear stream")
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards after plan reuse = %d, want 1", len(ff.patchedCards))
	}
	if body := cardMarkdownContent(t, ff.patchedCards[0]); !strings.Contains(body, "计划:\n- [in_progress] run") {
		t.Fatalf("patched plan body = %q", body)
	}
}
