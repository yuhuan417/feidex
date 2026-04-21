package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
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

func TestSessionEmitsTextFromAssistantMessage(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number:        1,
		Tools:         map[string]*toolState{},
		SeenAssistant: map[string]bool{},
	}

	session.turns[1] = session.current
	session.handleAssistantMessage(wireAssistantMessage{
		Type: "assistant",
		Message: wireMessageContent{
			ID:   "msg-1",
			Role: "assistant",
			Content: wireFlexibleContent{
				raw: []byte(`[{"type":"text","text":"hello world"}]`),
			},
		},
	})

	event := readTextEvent(t, session.Events())
	if event.Text != "hello world" || event.FullText != "hello world" {
		t.Fatalf("TextEvent = %#v, want hello world", event)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionDeduplicatesRepeatedAssistantTextForSameMessageBlock(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number:        1,
		Tools:         map[string]*toolState{},
		SeenAssistant: map[string]bool{},
	}
	session.turns[1] = session.current

	msg := wireAssistantMessage{
		Type: "assistant",
		Message: wireMessageContent{
			ID:   "msg-1",
			Role: "assistant",
			Content: wireFlexibleContent{
				raw: []byte(`[{"type":"text","text":"hello"}]`),
			},
		},
	}
	session.handleAssistantMessage(msg)
	session.handleAssistantMessage(msg)

	event := readTextEvent(t, session.Events())
	if event.Text != "hello" {
		t.Fatalf("TextEvent = %#v, want hello", event)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionEmitsToolCompleteFromAssistantMessage(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number:        1,
		Tools:         map[string]*toolState{},
		SeenAssistant: map[string]bool{},
	}
	session.turns[1] = session.current

	session.handleAssistantMessage(wireAssistantMessage{
		Type: "assistant",
		Message: wireMessageContent{
			ID:   "msg-1",
			Role: "assistant",
			Content: wireFlexibleContent{
				raw: []byte(`[{"type":"tool_use","id":"tool-1","name":"Edit","input":{"file_path":"demo.go"}}]`),
			},
		},
	})

	event := readToolCompleteEvent(t, session.Events())
	if event.ID != "tool-1" || event.Name != "Edit" {
		t.Fatalf("ToolCompleteEvent = %#v", event)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionIgnoresContentBlockStreamEventsForDelivery(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number:        1,
		Tools:         map[string]*toolState{},
		SeenAssistant: map[string]bool{},
	}
	session.turns[1] = session.current

	session.handleStreamMessage(wireStreamMessage{
		Type:  "stream_event",
		Event: []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`),
	})
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionGetContextUsage(t *testing.T) {
	session := NewSession()
	session.started = true

	var stdout bytes.Buffer
	session.writer = newNDJSONWriter(&stdout)

	type result struct {
		usage ContextUsage
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		usage, err := session.GetContextUsage(context.Background())
		resultCh <- result{usage: usage, err: err}
	}()

	var request wireControlRequestToSend
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		payload := bytes.TrimSpace(stdout.Bytes())
		if len(payload) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatalf("unmarshal outbound request: %v", err)
		}
		break
	}
	if request.Type != "control_request" {
		t.Fatalf("request type = %q, want control_request", request.Type)
	}
	if request.RequestID == "" {
		t.Fatal("request id should not be empty")
	}

	session.handleLine([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"` + request.RequestID + `","response":{"totalTokens":2048,"maxTokens":8192,"percentage":25.0}}}`))

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("GetContextUsage() error = %v", got.err)
		}
		if got.usage.TotalTokens != 2048 || got.usage.MaxTokens != 8192 || got.usage.Percentage != 25.0 {
			t.Fatalf("GetContextUsage() = %#v, want total=2048 max=8192 percentage=25.0", got.usage)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for GetContextUsage result")
	}
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
