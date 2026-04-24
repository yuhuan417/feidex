package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestUsageFormattingHelpers(t *testing.T) {
	if got := formatUsageRatio(0, 0); got != "-" {
		t.Fatalf("formatUsageRatio(0,0) = %q, want -", got)
	}
	if got := formatContextUsedLine(73.25); got != "context used: 73.2%" {
		t.Fatalf("formatContextUsedLine(73.25) = %q", got)
	}
	if got := formatContextLeftLine(0, 1000); got != "context left: 100.0%" {
		t.Fatalf("formatContextLeftLine(zero total) = %q", got)
	}
	if got := formatContextLeftLine(750, 1000); got != "context left: 25.0%" {
		t.Fatalf("formatContextLeftLine(750/1000) = %q", got)
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
	if got := formatTurnElapsedLine((2 * time.Hour) + (3 * time.Minute) + (4 * time.Second)); got != "elapsed: 2h3m4s" {
		t.Fatalf("formatTurnElapsedLine(2h3m4s) = %q", got)
	}
	if got := formatTurnElapsedLine((26 * time.Hour) + (3 * time.Minute)); got != "elapsed: 1d2h3m" {
		t.Fatalf("formatTurnElapsedLine(26h3m) = %q", got)
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
			"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40},
			"modelContextWindow":1000
		}
	}`))
	if len(ff.replyCards) != 0 {
		t.Fatal("token usage notification should not send a separate card")
	}

	card := newUsageService(a).renderUsageCard(sessionKey)
	body := cardMarkdownContent(t, card)
	for _, want := range []string{
		"累计 token usage (`total`):",
		"- cache ratio: `50.0%`",
		"- total: `500`",
		"context left: 85.0%",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderUsageCard() missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "最近一次 turn (`last`)") {
		t.Fatalf("renderUsageCard() should not include last usage section:\n%s", body)
	}
}

func TestRenderUsageCardUsesClaudeModelUsageSnapshot(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	newUsageService(a).recordClaudeThreadUsage("thread-1", claudecli.TurnUsage{
		InputTokens:                   20,
		CacheCreationTokens:           10,
		CacheReadTokens:               100,
		CumulativeInputTokens:         7835,
		CumulativeCacheReadTokens:     28672,
		CumulativeCacheCreationTokens: 4096,
		CumulativeOutputTokens:        68,
		HasCumulativeUsage:            true,
		ContextWindow:                 1000,
		CostUSD:                       0.054077,
	})

	card := newUsageService(a).renderUsageCard(sessionKey)
	body := cardMarkdownContent(t, card)
	for _, want := range []string{
		"累计 token usage (`modelUsage`):",
		"- total: `40671`",
		"- input: `7835`",
		"- cache read: `28672`",
		"- cache write: `4096`",
		"- output: `68`",
		"- cost: `$0.054077`",
		"context used: 13.0%",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderUsageCard() missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "cache ratio") {
		t.Fatalf("Claude usage card should not render codex cache ratio fields:\n%s", body)
	}
}

func TestCommandUsageAndMenuAction(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-1", UserID: "user-1"}
	if err := newUsageService(a).commandUsage(msg, []string{"extra"}); err == nil {
		t.Fatal("expected commandUsage(args) to fail")
	}
	if err := newUsageService(a).commandUsage(msg, nil); err != nil {
		t.Fatalf("commandUsage() error = %v", err)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("expected commandUsage to send card")
	}
	resp, err := newMenuActionService(a).completeMenuUsage(&feishu.CardAction{}, a.makeSessionKey(msg))
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeMenuUsage() = %#v, %v", resp, err)
	}
}

func TestFinalAnswerSendsImmediatelyWithUsageFooter(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	newRuntimeStateService(a).bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	newRuntimeStateService(a).markTurnStartedAt("turn-1", time.Now().Add(-1500*time.Millisecond))

	a.handleNotification("thread/tokenUsage/updated", json.RawMessage(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":200,"inputTokens":150,"cachedInputTokens":90,"outputTokens":50,"reasoningOutputTokens":20},
			"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40},
			"modelContextWindow":1000
		}
	}`))
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "item-1", map[string]any{
		"type":  "agent_message",
		"text":  "final text",
		"phase": "final_answer",
	})

	if len(ff.replyCards) == 0 {
		t.Fatal("expected final card to be sent immediately")
	}
	card := ff.replyCards[len(ff.replyCards)-1]
	bodyMap := card["body"].(map[string]any)
	elements := bodyMap["elements"].([]map[string]any)
	if len(elements) < 2 {
		t.Fatalf("expected markdown plus compact footer element, got %#v", elements)
	}
	footerText := elements[len(elements)-1]["text"].(map[string]any)["content"].(string)
	if !strings.Contains(footerText, "context left: 85.0%") {
		t.Fatalf("context footer = %q", footerText)
	}
	if !strings.Contains(footerText, "elapsed:") {
		t.Fatalf("elapsed footer = %q", footerText)
	}
	if strings.Contains(footerText, "token: input") {
		t.Fatalf("expected token footer line to be omitted, got %q", footerText)
	}

	beforeComplete := len(ff.replyCards)
	a.finishTurn("thread-1", "turn-1", "completed")
	if len(ff.replyCards) != beforeComplete {
		t.Fatalf("expected completion not to replay final answer, got %d -> %d cards", beforeComplete, len(ff.replyCards))
	}
}

func TestFinalAnswerPrefersExactContextUsageFooter(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	newRuntimeStateService(a).bindTurnSubmission("thread-1", "turn-1", "sess-1", sub.ID)
	newRuntimeStateService(a).markTurnStartedAt("turn-1", time.Now().Add(-1500*time.Millisecond))

	a.handleNotification("thread/tokenUsage/updated", json.RawMessage(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":200,"inputTokens":150,"cachedInputTokens":90,"outputTokens":50,"reasoningOutputTokens":20},
			"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40},
			"modelContextWindow":1000
		}
	}`))
	newRuntimeStateService(a).recordTurnContextUsagePercent("turn-1", 73.25)
	newTurnStreamService(a).completeTurnItem(context.Background(), "thread-1", "turn-1", "item-1", map[string]any{
		"type":  "agent_message",
		"text":  "final text",
		"phase": "final_answer",
	})

	if len(ff.replyCards) == 0 {
		t.Fatal("expected final card to be sent immediately")
	}
	card := ff.replyCards[len(ff.replyCards)-1]
	bodyMap := card["body"].(map[string]any)
	elements := bodyMap["elements"].([]map[string]any)
	footerText := elements[len(elements)-1]["text"].(map[string]any)["content"].(string)
	if !strings.Contains(footerText, "context used: 73.2%") {
		t.Fatalf("context footer = %q", footerText)
	}
	if strings.Contains(footerText, "context left:") {
		t.Fatalf("footer should prefer exact context usage over derived context left: %q", footerText)
	}
}
