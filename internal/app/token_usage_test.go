package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestUsageFormattingHelpers(t *testing.T) {
	if got := formatUsageRatio(0, 0); got != "-" {
		t.Fatalf("formatUsageRatio(0,0) = %q, want -", got)
	}
	if got := formatTurnUsageLine(codexrpc.TokenUsageBreakdown{
		InputTokens:           150,
		CachedInputTokens:     90,
		OutputTokens:          50,
		ReasoningOutputTokens: 20,
	}); !strings.Contains(got, "cache 90 (60.0%)") {
		t.Fatalf("formatTurnUsageLine() = %q, want cache ratio", got)
	}
	if got := formatTurnElapsedLine(1500 * time.Millisecond); got != "elapsed: 1.5s" {
		t.Fatalf("formatTurnElapsedLine() = %q, want seconds", got)
	}
}

func TestRenderUsageCardAndStoreTokenUsage(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	a.handleNotification("thread/tokenUsage/updated", json.RawMessage(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":200,"inputTokens":150,"cachedInputTokens":90,"outputTokens":50,"reasoningOutputTokens":20},
			"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40}
		}
	}`))
	if len(ff.replyCards) != 0 {
		t.Fatal("token usage notification should not send a separate card")
	}

	card := a.renderUsageCard(sessionKey)
	body := cardMarkdownContent(t, card)
	for _, want := range []string{
		"累计 token usage (`total`):",
		"- cache ratio: `50.0%`",
		"最近一次 turn (`last`):",
		"- total: `200`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderUsageCard() missing %q:\n%s", want, body)
		}
	}
}

func TestCommandUsageAndMenuAction(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	if err := a.commandUsage(msg, []string{"extra"}); err == nil {
		t.Fatal("expected commandUsage(args) to fail")
	}
	if err := a.commandUsage(msg, nil); err != nil {
		t.Fatalf("commandUsage() error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected commandUsage to send card")
	}
	resp, err := a.completeMenuUsage(&feishu.CardAction{}, a.makeSessionKey(msg))
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeMenuUsage() = %#v, %v", resp, err)
	}
}

func TestCompletedTurnSendsFinalWithUsageFooter(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.markTurnStartedAt("turn-1", time.Now().Add(-1500*time.Millisecond))

	a.handleNotification("thread/tokenUsage/updated", json.RawMessage(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":200,"inputTokens":150,"cachedInputTokens":90,"outputTokens":50,"reasoningOutputTokens":20},
			"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40}
		}
	}`))
	a.completeTurnItem(context.Background(), "thread-1", "turn-1", "item-1", map[string]any{
		"type":  "agent_message",
		"text":  "final text",
		"phase": "final_answer",
	})
	a.finishTurn("thread-1", "turn-1", "completed")

	if len(ff.replyCards) == 0 {
		t.Fatal("expected final card to be sent on completion")
	}
	card := ff.replyCards[len(ff.replyCards)-1]
	bodyMap := card["body"].(map[string]any)
	elements := bodyMap["elements"].([]map[string]any)
	if len(elements) < 3 {
		t.Fatalf("expected markdown plus footer elements, got %#v", elements)
	}
	lastText := elements[len(elements)-2]["text"].(map[string]any)["content"].(string)
	if !strings.Contains(lastText, "usage: in 150 | cache 90 (60.0%) | out 50 | reasoning 20") {
		t.Fatalf("usage footer = %q", lastText)
	}
	elapsedText := elements[len(elements)-1]["text"].(map[string]any)["content"].(string)
	if !strings.Contains(elapsedText, "elapsed:") {
		t.Fatalf("elapsed footer = %q", elapsedText)
	}
}
