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

func TestSessionStoppedAndExitErrorAccessors(t *testing.T) {
	session := NewSession()
	session.stopped = true
	session.exitErr = context.DeadlineExceeded

	if !session.Stopped() {
		t.Fatal("Stopped() = false, want true")
	}
	if got := session.ExitError(); got != context.DeadlineExceeded {
		t.Fatalf("ExitError() = %v, want deadline exceeded", got)
	}
}

func TestSessionIgnoresContentBlockStreamEvents(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number: 1,
		Tools:  map[string]*toolState{},
	}

	session.handleStreamMessage(wireStreamMessage{
		Type:  "stream_event",
		Event: []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`),
	})
	session.handleStreamMessage(wireStreamMessage{
		Type:  "stream_event",
		Event: []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`),
	})
	session.handleStreamMessage(wireStreamMessage{
		Type:  "stream_event",
		Event: []byte(`{"type":"content_block_stop","index":0}`),
	})

	if got := session.current.FullText; got != "" {
		t.Fatalf("FullText after ignored content blocks = %q, want empty", got)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionEmitsToolCompleteOnceForRepeatedAssistantSnapshots(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number: 1,
		Tools:  map[string]*toolState{},
	}
	session.handleAssistantToolUseBlock(wireToolUseBlock{
		Type:  "tool_use",
		ID:    "tool-1",
		Name:  "Edit",
		Input: map[string]any{"file_path": "demo.go"},
	})
	session.handleAssistantToolUseBlock(wireToolUseBlock{
		Type:  "tool_use",
		ID:    "tool-1",
		Name:  "Edit",
		Input: map[string]any{"file_path": "demo.go"},
	})

	event := readToolCompleteEvent(t, session.Events())
	if event.ID != "tool-1" || event.Name != "Edit" {
		t.Fatalf("ToolCompleteEvent = %#v", event)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionEmitsTextFromAssistantSnapshots(t *testing.T) {
	session := NewSession()
	session.current = &turnState{Number: 1}

	session.handleAssistantTextBlock(wireTextBlock{
		Type: "text",
		Text: "hello",
	})
	session.handleAssistantTextBlock(wireTextBlock{
		Type: "text",
		Text: "hello world",
	})

	first := readTextEvent(t, session.Events())
	if first.Text != "hello" || first.FullText != "hello" {
		t.Fatalf("first TextEvent = %#v", first)
	}
	second := readTextEvent(t, session.Events())
	if second.Text != " world" || second.FullText != "hello world" {
		t.Fatalf("second TextEvent = %#v", second)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func readToolCompleteEvent(t *testing.T, events <-chan Event) ToolCompleteEvent {
	t.Helper()
	select {
	case raw := <-events:
		event, ok := raw.(ToolCompleteEvent)
		if !ok {
			t.Fatalf("event = %#v, want ToolCompleteEvent", raw)
		}
		return event
	default:
		t.Fatal("expected ToolCompleteEvent")
	}
	return ToolCompleteEvent{}
}

func readTextEvent(t *testing.T, events <-chan Event) TextEvent {
	t.Helper()
	select {
	case raw := <-events:
		event, ok := raw.(TextEvent)
		if !ok {
			t.Fatalf("event = %#v, want TextEvent", raw)
		}
		return event
	default:
		t.Fatal("expected TextEvent")
	}
	return TextEvent{}
}

func assertNoExtraSessionEvents(t *testing.T, events <-chan Event) {
	t.Helper()
	select {
	case raw := <-events:
		t.Fatalf("unexpected extra event: %#v", raw)
	default:
	}
}
