package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	appdelivery "feidex/internal/app/delivery"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func TestHistoryHelpersSummarizeTurnsAndInputs(t *testing.T) {
	phase := "final_answer"
	details := "more"
	turns := []codexrpc.ThreadReadTurn{
		{
			ID:     "turn-1",
			Status: "completed",
			Items: []codexrpc.ThreadReadItem{
				{
					Type: "userMessage",
					Content: json.RawMessage(`[
						{"type":"text","text":"hello"},
						{"type":"localImage","path":"/tmp/pic.png"},
						{"type":"skill","name":"openai-docs"}
					]`),
				},
				{Type: "agentMessage", Text: "answer-1", Phase: &phase},
			},
		},
		{
			ID:     "turn-2",
			Status: "failed",
			Error:  &codexrpc.ThreadReadTurnError{Message: "boom", AdditionalDetails: &details},
			Items: []codexrpc.ThreadReadItem{
				{Type: "userMessage", Content: json.RawMessage(`[{"type":"mention","path":"app://repo"}]`)},
			},
		},
	}

	summaries := summarizeThreadHistory(turns, "turn-2")
	if len(summaries) != 2 || !summaries[0].IsCurrent || summaries[0].TurnID != "turn-2" {
		t.Fatalf("summarizeThreadHistory() = %+v, want reversed with current turn first", summaries)
	}
	if !strings.Contains(summaries[1].InputPreview, "hello") || !strings.Contains(strings.Join(summaries[1].Inputs, "\n"), "[localImage] /tmp/pic.png") {
		t.Fatalf("summaries[1] = %+v, want rendered inputs", summaries[1])
	}
	if got := historyInputPreview([]string{"one", "two"}); !strings.Contains(got, "等 2 条") {
		t.Fatalf("historyInputPreview() = %q, want count suffix", got)
	}
	if got := historyUserMessageInputs(codexrpc.ThreadReadItem{Content: json.RawMessage(`bad`)}); got != nil {
		t.Fatalf("historyUserMessageInputs(invalid) = %+v, want nil", got)
	}
	if got := stringPtrValue(nil); got != "" {
		t.Fatalf("stringPtrValue(nil) = %q, want empty", got)
	}
}

func TestRenderHistoryCardsAndFetchCurrentThreadHistory(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	threadName := "Thread Name"
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/read" {
			t.Fatalf("unexpected method %q", method)
		}
		result := out.(*codexrpc.ThreadReadResult)
		result.Thread = codexrpc.ThreadReadThread{
			ID:   "thread-1",
			Name: &threadName,
			Turns: []codexrpc.ThreadReadTurn{
				{
					ID:     "turn-1",
					Status: "completed",
					Items: []codexrpc.ThreadReadItem{
						{Type: "userMessage", Content: json.RawMessage(`[{"type":"text","text":"hello"}]`)},
						{Type: "agentMessage", Text: "world"},
					},
				},
			},
		}
		_ = params
		return nil
	}

	card, err := newHistoryService(a).RenderHistoryCard(sessionKey, 0)
	if err != nil {
		t.Fatalf("renderHistoryCard() error = %v", err)
	}
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "当前线程: thread-1") || !strings.Contains(body, "当前页: `1-1 / 1`") || !strings.Contains(body, "在线下拉菜单中选择要查看的 turn。") {
		t.Fatalf("history card body = %q, want thread summary only", body)
	}
	selects := cardSelectStaticForTest(card)
	if len(selects) != 1 {
		t.Fatalf("history card selects = %+v, want 1 select", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 1 {
		t.Fatalf("history select options = %+v, want 1 option", options)
	}
	text, _ := options[0]["text"].(map[string]any)
	label, _ := text["content"].(string)
	if !strings.Contains(label, "Turn #1 | completed | hello") {
		t.Fatalf("history select label = %q, want status and input preview", label)
	}

	detail, err := newHistoryService(a).RenderHistoryDetailCard(sessionKey, 0)
	if err != nil {
		t.Fatalf("renderHistoryDetailCard() error = %v", err)
	}
	if detailBody := cardMarkdownContent(t, detail); !strings.Contains(detailBody, "回复：") || !strings.Contains(detailBody, "world") {
		t.Fatalf("history detail body = %q, want outputs", detailBody)
	}

	if _, _, _, err := newHistoryService(&App{}).FetchCurrentThreadHistory(sessionKey); err == nil {
		t.Fatal("expected fetchCurrentThreadHistory() without store to fail")
	}
	if _, err := newHistoryService(a).RenderHistoryDetailCard(sessionKey, 1); err == nil {
		t.Fatal("expected out-of-range detail index to fail")
	}
	if len(ff.replyCards) != 0 {
		t.Fatalf("unexpected feishu side effects: %+v", ff.replyCards)
	}
}

func TestHistoryPaginationUsesConfiguredPageSize(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/read" {
			t.Fatalf("unexpected method %q", method)
		}
		result := out.(*codexrpc.ThreadReadResult)
		result.Thread = codexrpc.ThreadReadThread{
			ID: "thread-1",
		}
		for i := 1; i <= historyPageSize+1; i++ {
			result.Thread.Turns = append(result.Thread.Turns, codexrpc.ThreadReadTurn{
				ID:     fmt.Sprintf("turn-%d", i),
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", Content: json.RawMessage(fmt.Sprintf(`[{"type":"text","text":"input-%d"}]`, i))},
					{Type: "agentMessage", Text: fmt.Sprintf("output-%d", i)},
				},
			})
		}
		_ = params
		return nil
	}

	card, err := newHistoryService(a).RenderHistoryCard(sessionKey, 1)
	if err != nil {
		t.Fatalf("renderHistoryCard(page=1) error = %v", err)
	}
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, fmt.Sprintf("当前页: `%d-%d / %d`", historyPageSize+1, historyPageSize+1, historyPageSize+1)) {
		t.Fatalf("history page body = %q, want second page range", body)
	}
	selects := cardSelectStaticForTest(card)
	if len(selects) != 1 {
		t.Fatalf("history page selects = %+v, want 1 select", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != 1 {
		t.Fatalf("history page options = %+v, want 1 option on second page", options)
	}
	text, _ := options[0]["text"].(map[string]any)
	label, _ := text["content"].(string)
	if !strings.Contains(label, "当前 · Turn #1 | completed | input-1") {
		t.Fatalf("history page option label = %q, want current turn on second page", label)
	}

	detail, err := newHistoryService(a).RenderHistoryDetailCard(sessionKey, historyPageSize)
	if err != nil {
		t.Fatalf("renderHistoryDetailCard(last) error = %v", err)
	}
	buttons := cardButtonsForTest(detail)
	if len(buttons) != 2 {
		t.Fatalf("detail buttons = %+v, want 2 buttons", buttons)
	}
	values, _ := buttons[1]["value"].(map[string]any)
	if len(values) == 0 {
		behaviors, _ := buttons[1]["behaviors"].([]map[string]any)
		if len(behaviors) > 0 {
			values, _ = behaviors[0]["value"].(map[string]any)
		}
	}
	switch got := values["page"].(type) {
	case int:
		if got != 1 {
			t.Fatalf("detail back page = %v, want 1", values["page"])
		}
	case float64:
		if got != 1 {
			t.Fatalf("detail back page = %v, want 1", values["page"])
		}
	default:
		t.Fatalf("detail back page type = %T, want int or float64", values["page"])
	}
}

func TestHistoryCardWithConfiguredPageSizeFitsFeishuCardLimits(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   fmt.Sprintf("turn-%d", historyPageSize),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "thread/read" {
			t.Fatalf("unexpected method %q", method)
		}
		result := out.(*codexrpc.ThreadReadResult)
		result.Thread = codexrpc.ThreadReadThread{
			ID: "thread-1",
		}
		for i := 1; i <= historyPageSize; i++ {
			result.Thread.Turns = append(result.Thread.Turns, codexrpc.ThreadReadTurn{
				ID:     fmt.Sprintf("turn-%d", i),
				Status: "completed",
				Items: []codexrpc.ThreadReadItem{
					{Type: "userMessage", Content: json.RawMessage(fmt.Sprintf(`[{"type":"text","text":"input-%d"}]`, i))},
				},
			})
		}
		_ = params
		return nil
	}

	card, err := newHistoryService(a).RenderHistoryCard(sessionKey, 0)
	if err != nil {
		t.Fatalf("renderHistoryCard() error = %v", err)
	}
	selects := cardSelectStaticForTest(card)
	if len(selects) != 1 {
		t.Fatalf("history card selects = %+v, want 1 select", selects)
	}
	options, _ := selects[0]["options"].([]map[string]any)
	if len(options) != historyPageSize {
		t.Fatalf("history card options = %d, want %d", len(options), historyPageSize)
	}
	if got := appdelivery.CountCardComponentNodes(card); got >= feishuReplyCardMaxComponentCount {
		t.Fatalf("history card component count = %d, want < %d", got, feishuReplyCardMaxComponentCount)
	}
	payload, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal(card) error = %v", err)
	}
	if len(payload) > feishuReplyCardMaxPayloadBytes {
		t.Fatalf("history card payload = %d, want <= %d", len(payload), feishuReplyCardMaxPayloadBytes)
	}
}
