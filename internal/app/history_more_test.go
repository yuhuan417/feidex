package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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

	card, err := a.renderHistoryCard(sessionKey, 0)
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

	detail, err := a.renderHistoryDetailCard(sessionKey, 0)
	if err != nil {
		t.Fatalf("renderHistoryDetailCard() error = %v", err)
	}
	if detailBody := cardMarkdownContent(t, detail); !strings.Contains(detailBody, "回复：") || !strings.Contains(detailBody, "world") {
		t.Fatalf("history detail body = %q, want outputs", detailBody)
	}

	if _, _, _, err := (&App{}).fetchCurrentThreadHistory(sessionKey); err == nil {
		t.Fatal("expected fetchCurrentThreadHistory() without store to fail")
	}
	if _, err := a.renderHistoryDetailCard(sessionKey, 1); err == nil {
		t.Fatal("expected out-of-range detail index to fail")
	}
	if len(ff.replyCards) != 0 {
		t.Fatalf("unexpected feishu side effects: %+v", ff.replyCards)
	}
}
