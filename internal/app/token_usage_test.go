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
	if got := formatContextRemainingLine(0, 1000); got != "context remaining: 100.0%" {
		t.Fatalf("formatContextRemainingLine(zero total) = %q", got)
	}
	if got := formatContextRemainingLine(750, 1000); got != "context remaining: 25.0%" {
		t.Fatalf("formatContextRemainingLine(750/1000) = %q", got)
	}
	if got := formatTurnUsageLine(codexrpc.TokenUsageBreakdown{
		InputTokens:           150,
		CachedInputTokens:     90,
		OutputTokens:          50,
		ReasoningOutputTokens: 20,
	}); !strings.Contains(got, "cache 90 (60.0%)") {
		t.Fatalf("formatTurnUsageLine() = %q, want cache ratio", got)
	}
	if got := formatTurnElapsedLine(400 * time.Millisecond); got != "elapsed: <1s" {
		t.Fatalf("formatTurnElapsedLine(<1s) = %q", got)
	}
	if got := formatTurnElapsedLine(1500 * time.Millisecond); got != "elapsed: 2s" {
		t.Fatalf("formatTurnElapsedLine(1500ms) = %q", got)
	}
	if got := formatTurnElapsedLine((2*time.Hour)+(3*time.Minute)+(4*time.Second)); got != "elapsed: 2h3m4s" {
		t.Fatalf("formatTurnElapsedLine(2h3m4s) = %q", got)
	}
	if got := formatTurnElapsedLine((26*time.Hour)+(3*time.Minute)); got != "elapsed: 1d2h3m" {
		t.Fatalf("formatTurnElapsedLine(26h3m) = %q", got)
	}
}

func TestRenderUsageCardAndStoreTokenUsage(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.codex.(*fakeCodexClient).callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "config/read" {
			t.Fatalf("unexpected codex call: %s", method)
		}
		resp := out.(*codexrpc.ConfigReadResponse)
		limit := int64(1000)
		resp.Config.ModelAutoCompactTokenLimit = &limit
		return nil
	}
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
		"- total: `500`",
		"context remaining: 50.0%",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderUsageCard() missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "最近一次 turn (`last`)") {
		t.Fatalf("renderUsageCard() should not include last usage section:\n%s", body)
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
	a.codex.(*fakeCodexClient).callHook = func(_ context.Context, method string, _ any, _ any) error {
		t.Fatalf("unexpected codex call: %s", method)
		return nil
	}
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	a.markTurnStartedAt("turn-1", time.Now().Add(-1500*time.Millisecond))
	limit := int64(1000)
	a.autoCompact = map[string]*int64{
		"thread-1": &limit,
	}

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
	if len(elements) < 2 {
		t.Fatalf("expected markdown plus compact footer element, got %#v", elements)
	}
	footerText := elements[len(elements)-1]["text"].(map[string]any)["content"].(string)
	if !strings.Contains(footerText, "context remaining: 50.0%") {
		t.Fatalf("context footer = %q", footerText)
	}
	if !strings.Contains(footerText, "elapsed:") {
		t.Fatalf("elapsed footer = %q", footerText)
	}
	if strings.Contains(footerText, "token: input") {
		t.Fatalf("expected token footer line to be omitted, got %q", footerText)
	}
}
