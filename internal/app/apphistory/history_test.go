package apphistory

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
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

	summaries := SummarizeThreadHistory(turns, "turn-2")
	if len(summaries) != 2 || !summaries[0].IsCurrent || summaries[0].TurnID != "turn-2" {
		t.Fatalf("SummarizeThreadHistory() = %+v, want reversed with current turn first", summaries)
	}
	if !strings.Contains(summaries[1].InputPreview, "hello") || !strings.Contains(strings.Join(summaries[1].Inputs, "\n"), "[localImage] /tmp/pic.png") {
		t.Fatalf("summaries[1] = %+v, want rendered inputs", summaries[1])
	}
	if got := InputPreview([]string{"one", "two"}); !strings.Contains(got, "等 2 条") {
		t.Fatalf("InputPreview() = %q, want count suffix", got)
	}
	if got := UserMessageInputs(codexrpc.ThreadReadItem{Content: json.RawMessage(`bad`)}); got != nil {
		t.Fatalf("UserMessageInputs(invalid) = %+v, want nil", got)
	}
	if got := StringPtrValue(nil); got != "" {
		t.Fatalf("StringPtrValue(nil) = %q, want empty", got)
	}
}
