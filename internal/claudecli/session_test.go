package claudecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
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
		WithEffort("max"),
		WithMCPConfigPath("/tmp/mcp.json"),
		WithPermissionMode(PermissionModePlan),
		WithDangerouslySkipPermissions(),
		WithDisablePlugins(),
		WithPermissionPromptToolStdio(),
		WithSystemPrompt("system prompt"),
		WithResume("session-123"),
		WithForkSession(),
	)

	got := session.cliArgs()
	want := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--model", "sonnet",
		"--effort", "max",
		"--permission-mode", "plan",
		"--dangerously-skip-permissions",
		"--plugin-dir", "/dev/null",
		"--permission-prompt-tool", "stdio",
		"--system-prompt", "system prompt",
		"--resume", "session-123",
		"--fork-session",
		"--mcp-config", "/tmp/mcp.json",
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

func TestSessionInitializeSendsControlInitializeAndWaitsForResponse(t *testing.T) {
	session := NewSession()
	session.started = true
	var out bytes.Buffer
	session.writer = newNDJSONWriter(&out)

	done := make(chan error, 1)
	go func() {
		done <- session.Initialize(context.Background())
	}()

	deadline := time.Now().Add(time.Second)
	requestID := ""
	for time.Now().Before(deadline) {
		session.mu.Lock()
		for id := range session.pendingCtl {
			requestID = id
			break
		}
		session.mu.Unlock()
		if requestID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if requestID == "" {
		t.Fatal("Initialize() did not register a pending control request")
	}

	session.handleControlResponse(wireControlResponse{
		Type: "control_response",
		Response: wireControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response:  map[string]any{"pid": 123},
		},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Initialize() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Initialize() did not return after control response")
	}

	if got := out.String(); !strings.Contains(got, `"subtype":"initialize"`) {
		t.Fatalf("Initialize() wrote %q, want initialize control request", got)
	}
}

func TestSessionInitializeReturnsProcessExitWhenSessionStopsBeforeResponse(t *testing.T) {
	session := NewSession()
	session.started = true
	var out bytes.Buffer
	session.writer = newNDJSONWriter(&out)

	done := make(chan error, 1)
	go func() {
		done <- session.Initialize(context.Background())
	}()

	_ = waitForPendingControlRequest(t, session)

	session.mu.Lock()
	session.stopped = true
	session.mu.Unlock()
	close(session.waitDone)
	session.closeEvents()

	select {
	case err := <-done:
		var procErr *ProcessError
		if !errors.As(err, &procErr) {
			t.Fatalf("Initialize() error = %v, want ProcessError", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Initialize() did not return after session exit")
	}
}

func TestSessionSetModelSendsControlRequestAndUpdatesConfig(t *testing.T) {
	session := NewSession()
	session.started = true
	var out bytes.Buffer
	session.writer = newNDJSONWriter(&out)

	done := make(chan error, 1)
	go func() {
		done <- session.SetModel(context.Background(), "opus")
	}()

	requestID := waitForPendingControlRequest(t, session)
	session.handleControlResponse(wireControlResponse{
		Type: "control_response",
		Response: wireControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
		},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetModel() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SetModel() did not return after control response")
	}

	if got := out.String(); !strings.Contains(got, `"subtype":"set_model"`) || !strings.Contains(got, `"model":"opus"`) {
		t.Fatalf("SetModel() wrote %q, want set_model control request", got)
	}
	if got := session.cfg.Model; got != "opus" {
		t.Fatalf("session cfg model = %q, want opus", got)
	}
}

func TestSessionSetEffortSendsApplyFlagSettingsAndUpdatesConfig(t *testing.T) {
	session := NewSession()
	session.started = true
	var out bytes.Buffer
	session.writer = newNDJSONWriter(&out)

	done := make(chan error, 1)
	go func() {
		done <- session.SetEffort(context.Background(), "high")
	}()

	requestID := waitForPendingControlRequest(t, session)
	session.handleControlResponse(wireControlResponse{
		Type: "control_response",
		Response: wireControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
		},
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetEffort() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SetEffort() did not return after control response")
	}

	if got := out.String(); !strings.Contains(got, `"subtype":"apply_flag_settings"`) || !strings.Contains(got, `"effortLevel":"high"`) {
		t.Fatalf("SetEffort() wrote %q, want apply_flag_settings control request", got)
	}
	if got := session.cfg.Effort; got != "high" {
		t.Fatalf("session cfg effort = %q, want high", got)
	}
}

func TestSessionSetEffortRejectsDefaultHotApply(t *testing.T) {
	session := NewSession()
	session.started = true
	var out bytes.Buffer
	session.writer = newNDJSONWriter(&out)

	err := session.SetEffort(context.Background(), "")
	if err != ErrEffortDefaultHotApplyUnsupported {
		t.Fatalf("SetEffort(default) error = %v, want %v", err, ErrEffortDefaultHotApplyUnsupported)
	}
	if got := out.String(); got != "" {
		t.Fatalf("SetEffort(default) wrote %q, want empty output", got)
	}
}

func TestSessionStartStopsProcessWhenContextCanceled(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "claude.pid")
	scriptPath := filepath.Join(dir, "fake-claude.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
trap 'exit 0' TERM INT
while :; do
  sleep 1
done
`, pidPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := NewSession(
		WithCLIPath(scriptPath),
		WithEventBufferSize(1),
	)
	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	pid := waitForSessionPIDFile(t, pidPath)
	cancel()
	waitForSessionProcessExit(t, pid)
}

func waitForPendingControlRequest(t *testing.T, session *Session) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		for id := range session.pendingCtl {
			session.mu.Unlock()
			return id
		}
		session.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("control request was not registered")
	return ""
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

func TestSessionEmitsToolStartedFromAssistantMessage(t *testing.T) {
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

	event := readToolStartedEvent(t, session.Events())
	if event.ID != "tool-1" || event.Name != "Edit" {
		t.Fatalf("ToolStartedEvent = %#v", event)
	}
	if got := event.Input["file_path"]; got != "demo.go" {
		t.Fatalf("ToolStartedEvent input = %#v", event.Input)
	}
	assertNoExtraSessionEvents(t, session.Events())
}

func TestSessionEmitsToolCompleteFromUserToolResult(t *testing.T) {
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
	readToolStartedEvent(t, session.Events())

	session.handleUserMessage(wireUserMessage{
		Type: "user",
		Message: wireMessageContent{
			ID:   "msg-2",
			Role: "user",
			Content: wireFlexibleContent{
				raw: []byte(`[{"type":"tool_result","tool_use_id":"tool-1","content":"done","is_error":false}]`),
			},
		},
	})

	event := readToolCompleteEvent(t, session.Events())
	if event.ID != "tool-1" || event.Name != "Edit" {
		t.Fatalf("ToolCompleteEvent = %#v", event)
	}
	if got := event.Input["file_path"]; got != "demo.go" {
		t.Fatalf("ToolCompleteEvent input = %#v", event.Input)
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

func TestSessionHandleResultMessageIncludesContextWindow(t *testing.T) {
	session := NewSession()
	session.current = &turnState{
		Number:        1,
		Tools:         map[string]*toolState{},
		SeenAssistant: map[string]bool{},
	}
	session.turns[1] = session.current

	session.handleResultMessage(wireResultMessage{
		Type: "result",
		Usage: wireUsage{
			InputTokens:              3,
			CacheCreationInputTokens: 4603,
			CacheReadInputTokens:     18841,
			OutputTokens:             122,
		},
		ModelUsage: map[string]wireModelUsage{
			"claude-haiku-4-5-20251001": {
				InputTokens:              209,
				OutputTokens:             18,
				CacheReadInputTokens:     0,
				CacheCreationInputTokens: 0,
				ContextWindow:            200000,
			},
			"claude-sonnet-4-5-20250929": {
				InputTokens:              3,
				OutputTokens:             122,
				CacheReadInputTokens:     18841,
				CacheCreationInputTokens: 4603,
				ContextWindow:            200000,
			},
		},
	})

	select {
	case raw := <-session.Events():
		event, ok := raw.(TurnCompleteEvent)
		if !ok {
			t.Fatalf("event = %#v, want TurnCompleteEvent", raw)
		}
		if event.Usage.InputTokens != 3 || event.Usage.CacheCreationTokens != 4603 || event.Usage.CacheReadTokens != 18841 || event.Usage.OutputTokens != 122 {
			t.Fatalf("usage = %#v", event.Usage)
		}
		if !event.Usage.HasCumulativeUsage {
			t.Fatalf("expected cumulative model usage in %#v", event.Usage)
		}
		if event.Usage.CumulativeInputTokens != 212 || event.Usage.CumulativeOutputTokens != 140 || event.Usage.CumulativeCacheReadTokens != 18841 || event.Usage.CumulativeCacheCreationTokens != 4603 {
			t.Fatalf("cumulative usage = %#v", event.Usage)
		}
		if event.Usage.ContextWindow != 200000 {
			t.Fatalf("context window = %d, want 200000", event.Usage.ContextWindow)
		}
	case <-time.After(time.Second):
		t.Fatal("expected TurnCompleteEvent")
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

func readToolStartedEvent(t *testing.T, events <-chan Event) ToolStartedEvent {
	t.Helper()
	select {
	case raw := <-events:
		event, ok := raw.(ToolStartedEvent)
		if !ok {
			t.Fatalf("event = %#v, want ToolStartedEvent", raw)
		}
		return event
	default:
		t.Fatal("expected ToolStartedEvent")
	}
	return ToolStartedEvent{}
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

func waitForSessionPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s", path)
	return 0
}

func waitForSessionProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !sessionProcessAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d still alive", pid)
}

func sessionProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
