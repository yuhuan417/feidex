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
	if got := cardHeaderTitle(t, ff.patchedCards[1]); !strings.Contains(got, "反馈中") {
		t.Fatalf("patched reply card title = %q, want to contain 反馈中", got)
	}
	if got := cardHeaderTemplate(t, ff.patchedCards[1]); got != "blue" {
		t.Fatalf("patched reply card template = %q, want blue", got)
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

func TestTurnItemCardsPrefixWorkspaceAndPlan(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if _, err := a.store.UpdateSession("sess-1", func(sess *state.Session) {
		sess.ActiveThreadCollaborationMode = &state.SessionCollaborationMode{Mode: "plan", Model: "gpt-5.4"}
	}); err != nil {
		t.Fatalf("UpdateSession(plan mode) error = %v", err)
	}

	replyCard := newOutboundCardService(a).renderTurnItemCard(context.Background(), sub, turnItemCardPayload{
		ItemType:      "agent_message",
		Title:         "最终答复",
		Color:         "green",
		SummaryText:   "reply body",
		IsFinalAnswer: true,
	}, false)
	if got := cardHeaderTitle(t, replyCard); got != "["+a.cfg.Workspaces[0].ID+"] [plan] 最终答复" {
		t.Fatalf("reply item title = %q", got)
	}
	if body := cardMarkdownContent(t, replyCard); strings.Contains(body, "当前模式: plan") {
		t.Fatalf("reply item body = %q, want no plan banner", body)
	}

	compactCard := newOutboundCardService(a).renderTurnItemCard(context.Background(), sub, turnItemCardPayload{
		ItemType:    "command_execution",
		Title:       "命令执行",
		Color:       "blue",
		SummaryText: "command body",
	}, false)
	if got := cardHeaderTitle(t, compactCard); got != "["+a.cfg.Workspaces[0].ID+"] [plan] 命令执行" {
		t.Fatalf("compact item title = %q", got)
	}
	if body := cardMarkdownContent(t, compactCard); strings.Contains(body, "当前模式: plan") {
		t.Fatalf("compact item body = %q, want no plan banner", body)
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
	got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, turnItemCardPayload{
		ItemID:        "item-final",
		ItemType:      "agent_message",
		Title:         "最终答复",
		Color:         "green",
		SummaryText:   "intro\n\n" + longParagraph + "\n\n" + longParagraph,
		IsFinalAnswer: true,
	}, "")
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
