package threadview

import (
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
)

// actionRows returns, for every action row in the card, the labels it holds.
func actionRows(card map[string]any) [][]string {
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	rows := make([][]string, 0, len(elements))
	for _, element := range elements {
		columns, _ := element["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		row := make([]string, 0, len(columns))
		for _, column := range columns {
			children, _ := column["elements"].([]map[string]any)
			for _, child := range children {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				text, _ := child["text"].(map[string]any)
				label, _ := text["content"].(string)
				row = append(row, label)
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// TestConversationThreadsCardStacksItsControls keeps the thread page readable:
// it owns up to five controls plus the back control, so each has to keep its
// own row. Rendering them through MarkdownPageCard.Buttons squeezes every one
// of them into a single row, which is how this card regressed before.
func TestConversationThreadsCardStacksItsControls(t *testing.T) {
	controls := []feishu.Button{
		{Text: "new thread", Type: "default", Value: map[string]any{"action": "thread.new.start"}},
		{Text: "fork thread", Type: "default", Value: map[string]any{"action": "thread.fork.start"}},
		{Text: "配置沙箱", Type: "default", Value: map[string]any{"action": "thread.sandbox.menu"}},
		{Text: "配置策略", Type: "default", Value: map[string]any{"action": "thread.policy.menu"}},
		{Text: "配置多智能体模式", Type: "default", Value: map[string]any{"action": "thread.multiagent.menu"}},
	}
	card := BuildConversationThreadsCard("sess-1", ConversationThreadsCardView{
		Title:     "线程管理",
		BodyLines: []string{"当前线程: `thread-1`"},
		Buttons:   controls,
		Items:     nil,
	})

	rows := actionRows(card)
	if len(rows) != len(controls)+1 {
		t.Fatalf("action rows = %d %q, want one row per control plus the back control (%d)", len(rows), rows, len(controls)+1)
	}
	for i, control := range controls {
		if len(rows[i]) != 1 || rows[i][0] != control.Text {
			t.Fatalf("row %d = %q, want just %q", i, rows[i], control.Text)
		}
	}
	backRow := rows[len(rows)-1]
	if len(backRow) != 1 || !feishu.IsMenuBackButtonText(backRow[0]) {
		t.Fatalf("last row = %q, want the back control alone", backRow)
	}
	if body, _ := cardBodyText(card); !strings.Contains(body, "当前位置：") {
		t.Fatalf("card body = %q, want a declared breadcrumb", body)
	}
}

func cardBodyText(card map[string]any) (string, bool) {
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	for _, element := range elements {
		if content, ok := element["content"].(string); ok {
			return content, true
		}
	}
	return "", false
}

// TestCodexThreadsCardRendersEveryControlWithAnActiveThread renders the codex
// card through its real entrypoint: a session with an active thread must show
// all five controls, each on its own row, followed by the back control.
func TestCodexThreadsCardRendersEveryControlWithAnActiveThread(t *testing.T) {
	sess := &conversation.Session{Key: "sess-1", ActiveThreadID: "thread-1"}
	card, err := RenderCodexThreadsCard("sess-1", sess, config.Workspace{ID: "default"}, "codex", nil, false)
	if err != nil {
		t.Fatalf("RenderCodexThreadsCard() error = %v", err)
	}
	rows := actionRows(card)
	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, row...)
	}
	for _, want := range []string{"new thread", "fork thread", "配置沙箱", "配置策略", "配置多智能体模式"} {
		found := false
		for _, label := range labels {
			if strings.Contains(label, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("codex card controls = %q, missing %q", labels, want)
		}
	}
	if !feishu.IsMenuBackButtonText(labels[len(labels)-1]) {
		t.Fatalf("codex card last control = %q, want the back control", labels[len(labels)-1])
	}
	for i, row := range rows {
		if len(row) != 1 {
			t.Fatalf("row %d = %q, want one control per row", i, row)
		}
	}
}
