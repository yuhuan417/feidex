package app

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
)

func TestRenderThreadTokenUsageBody(t *testing.T) {
	window := int64(1000)
	body := renderThreadTokenUsageBody("turn-1", codexrpc.ThreadTokenUsage{
		Last: codexrpc.TokenUsageBreakdown{
			TotalTokens:           200,
			InputTokens:           150,
			CachedInputTokens:     90,
			OutputTokens:          50,
			ReasoningOutputTokens: 20,
		},
		Total: codexrpc.TokenUsageBreakdown{
			TotalTokens:           500,
			InputTokens:           400,
			CachedInputTokens:     200,
			OutputTokens:          100,
			ReasoningOutputTokens: 40,
		},
		ModelContextWindow: &window,
	})
	for _, want := range []string{
		"turn: `turn-1`",
		"- total: `200`",
		"- cache ratio: `60.0%`",
		"- context usage: `50.0%`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderThreadTokenUsageBody() missing %q:\n%s", want, body)
		}
	}
	if got := formatUsageRatio(0, 0); got != "-" {
		t.Fatalf("formatUsageRatio(0,0) = %q, want -", got)
	}
}

func TestThreadTokenUsageNotificationSendsSeparateCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.handleNotification("thread/tokenUsage/updated", json.RawMessage(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":200,"inputTokens":150,"cachedInputTokens":90,"outputTokens":50,"reasoningOutputTokens":20},
			"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40},
			"modelContextWindow":1000
		}
	}`))

	if len(ff.replyCards) == 0 {
		t.Fatal("expected token usage notification to send a separate card")
	}
	body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	if !strings.Contains(body, "cache ratio") || !strings.Contains(body, "60.0%") {
		t.Fatalf("token usage card body = %q, want cache ratio", body)
	}

	a.cfg.Feishu.Quiet = true
	a.handleNotification("thread/tokenUsage/updated", json.RawMessage(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":10,"inputTokens":10,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0},
			"total":{"totalTokens":10,"inputTokens":10,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0}
		}
	}`))
	if len(ff.replyCards) < 2 {
		t.Fatal("expected token usage card to still be delivered in quiet mode")
	}
}
