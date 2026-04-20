package claudecli

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestParseQuestionsFromInput(t *testing.T) {
	input := map[string]any{
		"questions": []any{
			map[string]any{
				"question":    "Choose a mode",
				"multiSelect": true,
				"options": []any{
					"Fast",
					map[string]any{"label": "Safe"},
				},
			},
		},
	}

	questions, err := ParseQuestionsFromInput(input)
	if err != nil {
		t.Fatalf("ParseQuestionsFromInput() error = %v", err)
	}

	want := []Question{
		{
			Text:        "Choose a mode",
			MultiSelect: true,
			Options: []QuestionOption{
				{Label: "Fast"},
				{Label: "Safe"},
			},
		},
	}
	if !reflect.DeepEqual(questions, want) {
		t.Fatalf("ParseQuestionsFromInput() = %#v, want %#v", questions, want)
	}
}

func TestParsePlanInfoFromInput(t *testing.T) {
	input := map[string]any{
		"plan": "1. inspect\n2. patch",
		"allowedPrompts": []any{
			map[string]any{"tool": "Bash", "prompt": "run tests"},
			map[string]any{"tool": "Write", "prompt": "edit files"},
		},
	}

	info, err := ParsePlanInfoFromInput(input)
	if err != nil {
		t.Fatalf("ParsePlanInfoFromInput() error = %v", err)
	}

	want := PlanInfo{
		Plan: "1. inspect\n2. patch",
		AllowedPrompts: []AllowedPrompt{
			{Tool: "Bash", Prompt: "run tests"},
			{Tool: "Write", Prompt: "edit files"},
		},
	}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("ParsePlanInfoFromInput() = %#v, want %#v", info, want)
	}
}

func TestSessionCLIArgs(t *testing.T) {
	session := NewSession(
		WithModel("sonnet"),
		WithPermissionMode(PermissionModePlan),
		WithDisablePlugins(),
		WithPermissionPromptToolStdio(),
		WithSystemPrompt("system prompt"),
		WithResume("session-123"),
	)

	got := session.cliArgs()
	want := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--model", "sonnet",
		"--permission-mode", "plan",
		"--plugin-dir", "/dev/null",
		"--permission-prompt-tool", "stdio",
		"--system-prompt", "system prompt",
		"--resume", "session-123",
		"--include-partial-messages",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cliArgs() = %#v, want %#v", got, want)
	}
}

func TestSessionQueuesPendingTurnsWithoutReplacingCurrent(t *testing.T) {
	session := NewSession()
	session.started = true
	session.writer = newNDJSONWriter(&bytes.Buffer{})

	firstTurn, err := session.SendMessage(context.Background(), "first")
	if err != nil {
		t.Fatalf("SendMessage(first) error = %v", err)
	}
	secondTurn, err := session.SendMessage(context.Background(), "second")
	if err != nil {
		t.Fatalf("SendMessage(second) error = %v", err)
	}
	if firstTurn != 1 || secondTurn != 2 {
		t.Fatalf("turn numbers = %d,%d, want 1,2", firstTurn, secondTurn)
	}
	if session.current == nil || session.current.Number != 1 {
		t.Fatalf("current turn after second send = %+v, want first turn to stay active", session.current)
	}
	if !reflect.DeepEqual(session.pending, []int{2}) {
		t.Fatalf("pending turns = %#v, want [2]", session.pending)
	}

	session.handleStreamMessage(wireStreamMessage{
		Type:  "stream_event",
		Event: []byte(`{"type":"message_start"}`),
	})
	event := <-session.Events()
	started, ok := event.(TurnStartedEvent)
	if !ok || started.TurnNumber != 1 {
		t.Fatalf("message_start event = %#v, want TurnStartedEvent(1)", event)
	}

	session.handleResultMessage(wireResultMessage{Type: "result"})
	if session.current == nil || session.current.Number != 2 {
		t.Fatalf("current turn after first result = %+v, want second turn promoted", session.current)
	}
	if len(session.pending) != 0 {
		t.Fatalf("pending turns after first result = %#v, want empty", session.pending)
	}
}
